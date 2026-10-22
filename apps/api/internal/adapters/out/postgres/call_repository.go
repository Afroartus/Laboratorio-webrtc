package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

// callColumns es la proyección común de la tabla calls. Los identificadores
// salen como texto para que el dominio nunca vea tipos de PostgreSQL.
const callColumns = `id::text, room_name, owner_id::text, status, created_at, join_deadline, started_at, ended_at, max_duration_seconds, version`

type callRepository struct {
	db dbtx
}

func (r *callRepository) Insert(ctx context.Context, call domain.Call) error {
	const query = `
		INSERT INTO calls (id, room_name, owner_id, status, created_at, join_deadline, started_at, ended_at, max_duration_seconds, version)
		VALUES ($1::uuid, $2, $3::uuid, $4, $5, $6, $7, $8, $9, $10)`
	_, err := r.db.Exec(ctx, query,
		string(call.ID), call.RoomName, string(call.OwnerID), string(call.Status),
		call.CreatedAt, call.JoinDeadline, call.StartedAt, call.EndedAt,
		call.MaxDurationSeconds, call.Version)
	if err != nil {
		return fmt.Errorf("no se pudo guardar la llamada: %w", err)
	}
	return nil
}

func (r *callRepository) GetByID(ctx context.Context, id domain.CallID) (domain.Call, error) {
	row := r.db.QueryRow(ctx, `SELECT `+callColumns+` FROM calls WHERE id = $1::uuid`, string(id))
	return scanCall(row)
}

func (r *callRepository) GetByIDForUpdate(ctx context.Context, id domain.CallID) (domain.Call, error) {
	row := r.db.QueryRow(ctx, `SELECT `+callColumns+` FROM calls WHERE id = $1::uuid FOR UPDATE`, string(id))
	return scanCall(row)
}

func (r *callRepository) UpdateStatus(ctx context.Context, id domain.CallID, expectedVersion int, from, to domain.CallStatus, now time.Time) (int, error) {
	const query = `
		UPDATE calls
		SET status = $3,
		    version = version + 1,
		    started_at = CASE WHEN $3 = 'ACTIVE' THEN COALESCE(started_at, $4) ELSE started_at END,
		    ended_at = CASE WHEN $3 IN ('ENDED', 'CANCELLED') THEN $4 ELSE ended_at END
		WHERE id = $1::uuid AND status = $2 AND version = $5
		RETURNING version`
	var version int
	err := r.db.QueryRow(ctx, query, string(id), string(from), string(to), now, expectedVersion).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ports.ErrCallStateConflict
	}
	if err != nil {
		return 0, fmt.Errorf("no se pudo actualizar el estado de la llamada: %w", err)
	}
	return version, nil
}

func (r *callRepository) ListByStatus(ctx context.Context, status domain.CallStatus, limit int) ([]domain.Call, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+callColumns+` FROM calls WHERE status = $1 ORDER BY created_at LIMIT $2`,
		string(status), limit)
	if err != nil {
		return nil, fmt.Errorf("no se pudieron listar las llamadas: %w", err)
	}
	return collectCalls(rows)
}

func (r *callRepository) ListJoinDeadlineExceeded(ctx context.Context, now time.Time, limit int) ([]domain.Call, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+callColumns+` FROM calls WHERE status = 'CREATED' AND join_deadline < $1 ORDER BY join_deadline LIMIT $2`,
		now, limit)
	if err != nil {
		return nil, fmt.Errorf("no se pudieron listar las llamadas caducadas: %w", err)
	}
	return collectCalls(rows)
}

func (r *callRepository) ListDurationExceeded(ctx context.Context, now time.Time, limit int) ([]domain.Call, error) {
	rows, err := r.db.Query(ctx,
		`SELECT `+callColumns+` FROM calls
		 WHERE status = 'ACTIVE' AND started_at IS NOT NULL
		   AND started_at + (max_duration_seconds * INTERVAL '1 second') < $1
		 ORDER BY started_at LIMIT $2`,
		now, limit)
	if err != nil {
		return nil, fmt.Errorf("no se pudieron listar las llamadas que superaron su duración: %w", err)
	}
	return collectCalls(rows)
}

// scanCall traduce una fila de calls a la entidad de dominio.
func scanCall(row pgx.Row) (domain.Call, error) {
	var (
		call    domain.Call
		ownerID string
		status  string
	)
	err := row.Scan(&call.ID, &call.RoomName, &ownerID, &status, &call.CreatedAt, &call.JoinDeadline,
		&call.StartedAt, &call.EndedAt, &call.MaxDurationSeconds, &call.Version)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Call{}, ports.ErrCallNotFound
	}
	if err != nil {
		return domain.Call{}, fmt.Errorf("no se pudo leer la llamada: %w", err)
	}
	call.OwnerID = domain.UserID(ownerID)
	call.Status = domain.CallStatus(status)
	return call, nil
}

// collectCalls recorre las filas de una consulta de llamadas.
func collectCalls(rows pgx.Rows) ([]domain.Call, error) {
	defer rows.Close()
	calls := make([]domain.Call, 0)
	for rows.Next() {
		var (
			call    domain.Call
			ownerID string
			status  string
		)
		if err := rows.Scan(&call.ID, &call.RoomName, &ownerID, &status, &call.CreatedAt, &call.JoinDeadline,
			&call.StartedAt, &call.EndedAt, &call.MaxDurationSeconds, &call.Version); err != nil {
			return nil, fmt.Errorf("no se pudo leer la llamada: %w", err)
		}
		call.OwnerID = domain.UserID(ownerID)
		call.Status = domain.CallStatus(status)
		calls = append(calls, call)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("no se pudieron leer las llamadas: %w", err)
	}
	return calls, nil
}
