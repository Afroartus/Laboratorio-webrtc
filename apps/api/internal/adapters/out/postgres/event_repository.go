package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"webrtc/api/internal/domain"
)

type eventRepository struct {
	db dbtx
}

func (r *eventRepository) Append(ctx context.Context, event domain.CallEvent) error {
	details, err := marshalDetails(event.Details)
	if err != nil {
		return err
	}
	const query = `
		INSERT INTO call_events (id, call_id, event_type, actor_id, occurred_at, provider_event_id, details)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb)`
	_, err = r.db.Exec(ctx, query, event.ID, string(event.CallID), event.EventType,
		actorID(event.ActorID), event.OccurredAt, providerID(event.ProviderEventID), details)
	if err != nil {
		return fmt.Errorf("no se pudo guardar el evento de la llamada: %w", err)
	}
	return nil
}

// AppendProviderEvent guarda un evento del proveedor. Si su identificador ya
// existe lo descarta: los webhooks pueden repetirse.
func (r *eventRepository) AppendProviderEvent(ctx context.Context, event domain.CallEvent) (bool, error) {
	details, err := marshalDetails(event.Details)
	if err != nil {
		return false, err
	}
	const query = `
		INSERT INTO call_events (id, call_id, event_type, actor_id, occurred_at, provider_event_id, details)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7::jsonb)
		ON CONFLICT (provider_event_id) WHERE provider_event_id IS NOT NULL DO NOTHING`
	tag, err := r.db.Exec(ctx, query, event.ID, string(event.CallID), event.EventType,
		actorID(event.ActorID), event.OccurredAt, providerID(event.ProviderEventID), details)
	if err != nil {
		return false, fmt.Errorf("no se pudo guardar el evento de la llamada: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// marshalDetails convierte los detalles del evento a JSON.
func marshalDetails(details map[string]any) ([]byte, error) {
	if details == nil {
		return []byte(`{}`), nil
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return nil, fmt.Errorf("no se pudo codificar el evento de la llamada: %w", err)
	}
	return encoded, nil
}

// actorID traduce el actor opcional a un parámetro NULL cuando no existe.
func actorID(actor *domain.UserID) any {
	if actor == nil {
		return nil
	}
	return string(*actor)
}

// providerID traduce el identificador del proveedor a NULL cuando no existe.
func providerID(id string) any {
	if id == "" {
		return nil
	}
	return id
}
