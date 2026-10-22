package usecases

import (
	"context"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

// ActivateCallInput identifica la conexión que activa la llamada.
type ActivateCallInput struct {
	CallID         domain.CallID
	UserID         domain.UserID
	ParticipantSID string
	RoomSID        string
}

// ActivateCall pasa la llamada a ACTIVE cuando el primer participante conecta
// con los medios y registra la sesión de conexión.
type ActivateCall struct {
	uow   ports.UnitOfWork
	clock ports.Clock
	newID func() string
}

// NewActivateCall devuelve el caso de uso con sus dependencias.
func NewActivateCall(uow ports.UnitOfWork, clock ports.Clock, newID func() string) *ActivateCall {
	return &ActivateCall{uow: uow, clock: clock, newID: newID}
}

// Execute activa la llamada y guarda la sesión del participante.
func (s *ActivateCall) Execute(ctx context.Context, in ActivateCallInput) error {
	now := s.clock.Now()

	return s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		call, err := repos.Calls().GetByIDForUpdate(ctx, in.CallID)
		if err != nil {
			return err
		}
		if call.IsTerminal() {
			return ports.ErrCallEnded
		}

		session := domain.CallSession{
			ID:             s.newID(),
			CallID:         call.ID,
			UserID:         in.UserID,
			ParticipantSID: in.ParticipantSID,
			RoomSID:        in.RoomSID,
			JoinedAt:       now,
		}
		if err := repos.Sessions().Insert(ctx, session); err != nil {
			return err
		}

		// Ya estaba activa: solo se registra la conexión.
		if call.Status == domain.CallActive {
			return nil
		}
		if !call.CanTransitionTo(domain.CallActive) {
			return ports.ErrCallStateConflict
		}

		from := call.Status
		expectedVersion := call.Version
		if err := call.TransitionTo(domain.CallActive, now); err != nil {
			return err
		}
		if _, err := repos.Calls().UpdateStatus(ctx, call.ID, expectedVersion, from, domain.CallActive, now); err != nil {
			return err
		}

		actor := in.UserID
		event := domain.CallEvent{
			ID:         s.newID(),
			CallID:     call.ID,
			EventType:  "call.active",
			ActorID:    &actor,
			OccurredAt: now,
			Details:    map[string]any{"participant_sid": in.ParticipantSID},
		}
		return repos.Events().Append(ctx, event)
	})
}
