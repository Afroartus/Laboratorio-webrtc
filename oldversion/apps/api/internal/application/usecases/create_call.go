package usecases

import (
	"context"
	"errors"
	"time"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

// CreateCall crea una llamada con el actor como creador y un invitado local.
// La llamada, sus dos miembros y el evento de auditoría se guardan en la misma
// transacción: o existe todo, o no existe nada.
type CreateCall struct {
	uow          ports.UnitOfWork
	clock        ports.Clock
	newID        func() string
	maxDuration  time.Duration
	joinDeadline time.Duration
}

// NewCreateCall devuelve el caso de uso con sus dependencias.
func NewCreateCall(uow ports.UnitOfWork, clock ports.Clock, newID func() string, maxDuration, joinDeadline time.Duration) *CreateCall {
	return &CreateCall{
		uow:          uow,
		clock:        clock,
		newID:        newID,
		maxDuration:  maxDuration,
		joinDeadline: joinDeadline,
	}
}

// Execute crea la llamada y devuelve su identificador y estado.
func (s *CreateCall) Execute(ctx context.Context, in ports.CreateCallInput) (ports.CreateCallResult, error) {
	if in.ActorID == "" || in.InviteeID == "" || in.ActorID == in.InviteeID {
		return ports.CreateCallResult{}, ports.ErrInvalidInput
	}
	inviteeRole := in.InviteeRole
	if inviteeRole == "" {
		inviteeRole = domain.RoleParticipant
	}
	if !domain.IsValidRole(inviteeRole) || inviteeRole == domain.RoleCreator {
		return ports.CreateCallResult{}, ports.ErrInvalidInput
	}

	now := s.clock.Now()
	callID := domain.CallID(s.newID())

	err := s.uow.WithinTransaction(ctx, func(ctx context.Context, repos ports.Repositories) error {
		invitee, err := repos.Users().GetByID(ctx, in.InviteeID)
		if err != nil {
			if errors.Is(err, ports.ErrInvalidInput) {
				return ports.ErrInvalidInput
			}
			return err
		}
		if !invitee.Active {
			return ports.ErrInvalidInput
		}

		call := domain.Call{
			ID:                 callID,
			RoomName:           "call_" + string(callID),
			OwnerID:            in.ActorID,
			Status:             domain.CallCreated,
			CreatedAt:          now,
			JoinDeadline:       now.Add(s.joinDeadline),
			MaxDurationSeconds: int(s.maxDuration.Seconds()),
		}
		if err := repos.Calls().Insert(ctx, call); err != nil {
			return err
		}

		members := []domain.CallMember{
			{
				UserID:         in.ActorID,
				Role:           domain.RoleCreator,
				AllowedSources: domain.AllowedSourcesForRole(domain.RoleCreator),
				CreatedAt:      now,
			},
			{
				UserID:         in.InviteeID,
				Role:           inviteeRole,
				AllowedSources: domain.AllowedSourcesForRole(inviteeRole),
				CreatedAt:      now,
			},
		}
		if err := repos.Members().InsertMembers(ctx, call.ID, members); err != nil {
			return err
		}

		actor := in.ActorID
		event := domain.CallEvent{
			ID:         s.newID(),
			CallID:     call.ID,
			EventType:  "call.created",
			ActorID:    &actor,
			OccurredAt: now,
			Details: map[string]any{
				"invitee_id":   string(in.InviteeID),
				"invitee_role": string(inviteeRole),
			},
		}
		return repos.Events().Append(ctx, event)
	})
	if err != nil {
		return ports.CreateCallResult{}, err
	}
	return ports.CreateCallResult{CallID: callID, Status: domain.CallCreated}, nil
}
