package usecases

import (
	"context"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

// EndCall finaliza una llamada. Solo el creador puede hacerlo y repetir la
// operación no duplica efectos: la llamada queda en ENDING y el trabajo de
// eliminar la sala se guarda en la misma transacción.
type EndCall struct {
	uow   ports.UnitOfWork
	clock ports.Clock
	newID func() string
}

// NewEndCall devuelve el caso de uso con sus dependencias.
func NewEndCall(uow ports.UnitOfWork, clock ports.Clock, newID func() string) *EndCall {
	return &EndCall{uow: uow, clock: clock, newID: newID}
}

// Execute pide el cierre de la llamada y devuelve el estado resultante.
func (s *EndCall) Execute(ctx context.Context, in ports.CallAccess) (ports.EndCallResult, error) {
	now := s.clock.Now()
	var result ports.EndCallResult

	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		call, err := repos.Calls().GetByIDForUpdate(ctx, in.CallID)
		if err != nil {
			return err
		}
		if call.OwnerID != in.ActorID {
			return ports.ErrForbidden
		}
		// Ya terminada o ya en cierre: no se repite ningún efecto.
		if call.IsTerminal() || call.Status == domain.CallEnding {
			result = ports.EndCallResult{CallID: call.ID, Status: call.Status}
			return nil
		}
		if !call.CanTransitionTo(domain.CallEnding) {
			return ports.ErrCallStateConflict
		}

		from := call.Status
		expectedVersion := call.Version
		if err := call.TransitionTo(domain.CallEnding, now); err != nil {
			return err
		}
		if _, err := repos.Calls().UpdateStatus(ctx, call.ID, expectedVersion, from, domain.CallEnding, now); err != nil {
			return err
		}

		// El trabajo de eliminar la sala se guarda en la misma transacción que
		// el estado ENDING: si el proceso se reinicia, el trabajo sigue ahí.
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

		actor := in.ActorID
		event := domain.CallEvent{
			ID:         s.newID(),
			CallID:     call.ID,
			EventType:  "call.ending",
			ActorID:    &actor,
			OccurredAt: now,
			Details:    map[string]any{"from": string(from)},
		}
		if err := repos.Events().Append(ctx, event); err != nil {
			return err
		}

		result = ports.EndCallResult{CallID: call.ID, Status: domain.CallEnding}
		return nil
	})
	if err != nil {
		return ports.EndCallResult{}, err
	}
	return result, nil
}
