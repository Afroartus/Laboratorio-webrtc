package usecases

import (
	"context"
	"errors"
	"time"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

const (
	// outboxClaimLimit es el número máximo de trabajos que se reclaman en una
	// pasada.
	outboxClaimLimit = 50
	// outboxClaimLease es el tiempo durante el cual un trabajo reclamado no
	// vuelve a estar disponible.
	outboxClaimLease = time.Minute
)

// OutboxResult resume el resultado de una pasada del proceso de trabajos.
type OutboxResult struct {
	// Completed son los trabajos que se ejecutaron con éxito.
	Completed int
	// Failed son los trabajos que fallaron y se reintentarán más tarde.
	Failed int
	// Abandoned son los trabajos que agotaron sus reintentos.
	Abandoned int
}

// ProcessOutbox ejecuta los trabajos pendientes sobre el servidor de medios.
// La llamada al servidor queda fuera de la transacción SQL: solo el estado
// persistido se actualiza dentro de una transacción.
type ProcessOutbox struct {
	uow         ports.UnitOfWork
	clock       ports.Clock
	rooms       ports.RoomService
	maxAttempts int
	newID       func() string
}

// NewProcessOutbox devuelve el caso de uso con sus dependencias.
func NewProcessOutbox(uow ports.UnitOfWork, clock ports.Clock, rooms ports.RoomService, maxAttempts int, newID func() string) *ProcessOutbox {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	return &ProcessOutbox{uow: uow, clock: clock, rooms: rooms, maxAttempts: maxAttempts, newID: newID}
}

// Execute reclama los trabajos vencidos y los ejecuta.
func (s *ProcessOutbox) Execute(ctx context.Context) (OutboxResult, error) {
	now := s.clock.Now()
	var result OutboxResult

	var jobs []domain.OutboxJob
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		claimed, err := repos.Outbox().ClaimDue(ctx, now, outboxClaimLimit, outboxClaimLease)
		if err != nil {
			return err
		}
		jobs = claimed
		return nil
	})
	if err != nil {
		return result, err
	}

	for _, job := range jobs {
		if err := s.execute(ctx, job); err != nil {
			abandoned, failErr := s.recordFailure(ctx, job, err, now)
			if failErr != nil {
				return result, failErr
			}
			if abandoned {
				result.Abandoned++
				continue
			}
			result.Failed++
			continue
		}
		result.Completed++
		if err := s.recordSuccess(ctx, job, now); err != nil {
			return result, err
		}
	}
	return result, nil
}

// execute realiza el efecto externo del trabajo.
func (s *ProcessOutbox) execute(ctx context.Context, job domain.OutboxJob) error {
	switch job.Operation {
	case domain.OperationDeleteRoom:
		var roomName string
		err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
			call, err := repos.Calls().GetByID(ctx, job.CallID)
			if err != nil {
				return err
			}
			roomName = call.RoomName
			return nil
		})
		if err != nil {
			return err
		}
		return s.rooms.DeleteRoom(ctx, roomName)
	default:
		return errors.New("operación desconocida: " + string(job.Operation))
	}
}

// recordSuccess marca el trabajo como completado y cierra la llamada si estaba
// en ENDING.
func (s *ProcessOutbox) recordSuccess(ctx context.Context, job domain.OutboxJob, now time.Time) error {
	return s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		if err := repos.Outbox().MarkCompleted(ctx, job.ID, now); err != nil {
			return err
		}
		call, err := repos.Calls().GetByIDForUpdate(ctx, job.CallID)
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
			Details:    map[string]any{"reason": "room_deleted"},
		}
		return repos.Events().Append(ctx, event)
	})
}

// recordFailure guarda el error del trabajo y programa el siguiente intento. Si
// se agotan los reintentos, el trabajo se cierra y conserva el error. Devuelve
// si el trabajo quedó abandonado.
func (s *ProcessOutbox) recordFailure(ctx context.Context, job domain.OutboxJob, cause error, now time.Time) (bool, error) {
	attempts := job.Attempts + 1
	lastError := cause.Error()
	abandoned := attempts >= s.maxAttempts
	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		if abandoned {
			return repos.Outbox().MarkAbandoned(ctx, job.ID, attempts, now, lastError)
		}
		next := now.Add(domain.Backoff(attempts))
		return repos.Outbox().MarkFailed(ctx, job.ID, attempts, next, lastError)
	})
	if err != nil {
		return false, err
	}
	return abandoned, nil
}
