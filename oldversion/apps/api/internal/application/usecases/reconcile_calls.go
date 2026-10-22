package usecases

import (
	"context"
	"errors"
	"time"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

// Límite de llamadas revisadas en cada pasada del proceso periódico.
const reconcileBatch = 100

// ReconcileResult resume el trabajo de una pasada de reconciliación.
type ReconcileResult struct {
	// Cancelled son las llamadas vacías canceladas por vencer su plazo.
	Cancelled int
	// Ending son las llamadas activas que pasaron a ENDING.
	Ending int
	// Ended son las llamadas en cierre cuya sala ya no existe.
	Ended int
	// Orphans son las llamadas activas cuya sala no aparece en el servidor de
	// medios. Se registran pero no se cambian de estado: hasta que el paso 11
	// registre las conexiones de los participantes no hay forma de saber si la
	// sala se recreó.
	Orphans int
}

// ReconcileCalls compara el estado persistido con el estado real del servidor
// de medios y aplica las transiciones pendientes.
type ReconcileCalls struct {
	uow   ports.UnitOfWork
	clock ports.Clock
	rooms ports.RoomService
	newID func() string
}

// NewReconcileCalls devuelve el caso de uso con sus dependencias.
func NewReconcileCalls(uow ports.UnitOfWork, clock ports.Clock, rooms ports.RoomService, newID func() string) *ReconcileCalls {
	return &ReconcileCalls{uow: uow, clock: clock, rooms: rooms, newID: newID}
}

// Execute realiza una pasada de reconciliación.
func (s *ReconcileCalls) Execute(ctx context.Context) (ReconcileResult, error) {
	now := s.clock.Now()
	var result ReconcileResult

	expired, err := s.listJoinDeadlineExceeded(ctx, now)
	if err != nil {
		return result, err
	}
	for _, call := range expired {
		cancelled, err := s.cancel(ctx, call.ID, now)
		if err != nil {
			return result, err
		}
		if cancelled {
			result.Cancelled++
		}
	}

	overdue, err := s.listDurationExceeded(ctx, now)
	if err != nil {
		return result, err
	}
	for _, call := range overdue {
		ending, err := s.markEnding(ctx, call.ID, now)
		if err != nil {
			return result, err
		}
		if ending {
			result.Ending++
		}
	}

	closing, err := s.listByStatus(ctx, domain.CallEnding)
	if err != nil {
		return result, err
	}
	for _, call := range closing {
		_, err := s.rooms.GetRoom(ctx, call.RoomName)
		switch {
		case errors.Is(err, ports.ErrRoomNotFound):
			ended, err := s.markEnded(ctx, call.ID, now)
			if err != nil {
				return result, err
			}
			if ended {
				result.Ended++
			}
		case err != nil:
			return result, err
		}
	}

	active, err := s.listByStatus(ctx, domain.CallActive)
	if err != nil {
		return result, err
	}
	for _, call := range active {
		_, err := s.rooms.GetRoom(ctx, call.RoomName)
		switch {
		case errors.Is(err, ports.ErrRoomNotFound):
			result.Orphans++
		case err != nil:
			return result, err
		}
	}

	return result, nil
}

func (s *ReconcileCalls) listJoinDeadlineExceeded(ctx context.Context, now time.Time) ([]domain.Call, error) {
	var calls []domain.Call
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		found, err := repos.Calls().ListJoinDeadlineExceeded(ctx, now, reconcileBatch)
		if err != nil {
			return err
		}
		calls = found
		return nil
	})
	return calls, err
}

func (s *ReconcileCalls) listDurationExceeded(ctx context.Context, now time.Time) ([]domain.Call, error) {
	var calls []domain.Call
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		found, err := repos.Calls().ListDurationExceeded(ctx, now, reconcileBatch)
		if err != nil {
			return err
		}
		calls = found
		return nil
	})
	return calls, err
}

func (s *ReconcileCalls) listByStatus(ctx context.Context, status domain.CallStatus) ([]domain.Call, error) {
	var calls []domain.Call
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		found, err := repos.Calls().ListByStatus(ctx, status, reconcileBatch)
		if err != nil {
			return err
		}
		calls = found
		return nil
	})
	return calls, err
}

// cancel pasa a CANCELLED una llamada vacía cuyo plazo de ingreso venció.
func (s *ReconcileCalls) cancel(ctx context.Context, callID domain.CallID, now time.Time) (bool, error) {
	cancelled := false
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		call, err := repos.Calls().GetByIDForUpdate(ctx, callID)
		if err != nil {
			if errors.Is(err, ports.ErrCallNotFound) {
				return nil
			}
			return err
		}
		if !call.DeadlineExceeded(now) {
			return nil
		}
		from := call.Status
		expectedVersion := call.Version
		if err := call.TransitionTo(domain.CallCancelled, now); err != nil {
			return err
		}
		if _, err := repos.Calls().UpdateStatus(ctx, call.ID, expectedVersion, from, domain.CallCancelled, now); err != nil {
			return err
		}
		event := domain.CallEvent{
			ID:         s.newID(),
			CallID:     call.ID,
			EventType:  "call.cancelled",
			OccurredAt: now,
			Details:    map[string]any{"reason": "join_deadline_expired"},
		}
		if err := repos.Events().Append(ctx, event); err != nil {
			return err
		}
		cancelled = true
		return nil
	})
	return cancelled, err
}

// markEnding pasa a ENDING una llamada activa que superó su duración máxima y
// encola el trabajo de eliminar la sala.
func (s *ReconcileCalls) markEnding(ctx context.Context, callID domain.CallID, now time.Time) (bool, error) {
	ending := false
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		call, err := repos.Calls().GetByIDForUpdate(ctx, callID)
		if err != nil {
			if errors.Is(err, ports.ErrCallNotFound) {
				return nil
			}
			return err
		}
		if !call.DurationExceeded(now) {
			return nil
		}
		from := call.Status
		expectedVersion := call.Version
		if err := call.TransitionTo(domain.CallEnding, now); err != nil {
			return err
		}
		if _, err := repos.Calls().UpdateStatus(ctx, call.ID, expectedVersion, from, domain.CallEnding, now); err != nil {
			return err
		}
		job := domain.OutboxJob{
			ID:            s.newID(),
			CallID:        call.ID,
			Operation:     domain.OperationDeleteRoom,
			Attempts:      0,
			NextAttemptAt: now,
			CreatedAt:     now,
		}
		if err := repos.Outbox().Enqueue(ctx, job); err != nil {
			return err
		}
		event := domain.CallEvent{
			ID:         s.newID(),
			CallID:     call.ID,
			EventType:  "call.ending",
			OccurredAt: now,
			Details:    map[string]any{"reason": "max_duration_exceeded"},
		}
		if err := repos.Events().Append(ctx, event); err != nil {
			return err
		}
		ending = true
		return nil
	})
	return ending, err
}

// markEnded cierra la llamada cuando el servidor de medios ya no tiene la sala.
func (s *ReconcileCalls) markEnded(ctx context.Context, callID domain.CallID, now time.Time) (bool, error) {
	ended := false
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		call, err := repos.Calls().GetByIDForUpdate(ctx, callID)
		if err != nil {
			if errors.Is(err, ports.ErrCallNotFound) {
				return nil
			}
			return err
		}
		if call.Status != domain.CallEnding {
			return nil
		}
		from := call.Status
		expectedVersion := call.Version
		if err := call.TransitionTo(domain.CallEnded, now); err != nil {
			return err
		}
		if _, err := repos.Calls().UpdateStatus(ctx, call.ID, expectedVersion, from, domain.CallEnded, now); err != nil {
			return err
		}
		event := domain.CallEvent{
			ID:         s.newID(),
			CallID:     call.ID,
			EventType:  "call.ended",
			OccurredAt: now,
			Details:    map[string]any{"reason": "room_absent"},
		}
		if err := repos.Events().Append(ctx, event); err != nil {
			return err
		}
		ended = true
		return nil
	})
	return ended, err
}
