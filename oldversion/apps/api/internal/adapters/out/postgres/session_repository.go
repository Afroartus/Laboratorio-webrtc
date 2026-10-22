package postgres

import (
	"context"
	"fmt"
	"time"

	"webrtc/api/internal/domain"
)

type callSessionRepository struct {
	db dbtx
}

func (r *callSessionRepository) Insert(ctx context.Context, session domain.CallSession) error {
	const query = `
		INSERT INTO call_sessions (id, call_id, user_id, participant_sid, room_sid, joined_at, left_at, disconnect_reason)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4, $5, $6, $7, $8)`
	_, err := r.db.Exec(ctx, query, session.ID, string(session.CallID), string(session.UserID),
		session.ParticipantSID, session.RoomSID, session.JoinedAt, session.LeftAt, session.DisconnectReason)
	if err != nil {
		return fmt.Errorf("no se pudo guardar la sesión de la llamada: %w", err)
	}
	return nil
}

func (r *callSessionRepository) Close(ctx context.Context, id string, at time.Time, reason string) error {
	const query = `UPDATE call_sessions SET left_at = $2, disconnect_reason = $3 WHERE id = $1::uuid`
	if _, err := r.db.Exec(ctx, query, id, at, reason); err != nil {
		return fmt.Errorf("no se pudo cerrar la sesión de la llamada: %w", err)
	}
	return nil
}

func (r *callSessionRepository) ListOpenByCall(ctx context.Context, callID domain.CallID) ([]domain.CallSession, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id::text, call_id::text, user_id::text, participant_sid, room_sid, joined_at, left_at, disconnect_reason
		FROM call_sessions WHERE call_id = $1::uuid AND left_at IS NULL ORDER BY joined_at`, string(callID))
	if err != nil {
		return nil, fmt.Errorf("no se pudieron listar las sesiones de la llamada: %w", err)
	}
	defer rows.Close()
	sessions := make([]domain.CallSession, 0)
	for rows.Next() {
		var (
			session domain.CallSession
			callID  string
			userID  string
		)
		if err := rows.Scan(&session.ID, &callID, &userID, &session.ParticipantSID, &session.RoomSID,
			&session.JoinedAt, &session.LeftAt, &session.DisconnectReason); err != nil {
			return nil, fmt.Errorf("no se pudo leer la sesión de la llamada: %w", err)
		}
		session.CallID = domain.CallID(callID)
		session.UserID = domain.UserID(userID)
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("no se pudieron leer las sesiones de la llamada: %w", err)
	}
	return sessions, nil
}
