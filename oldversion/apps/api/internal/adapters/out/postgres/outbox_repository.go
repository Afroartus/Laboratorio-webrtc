package postgres

import (
	"context"
	"fmt"
	"time"

	"webrtc/api/internal/domain"
)

type outboxRepository struct {
	db dbtx
}

// Enqueue guarda un trabajo pendiente. Si ya existe uno igual sin completar no
// lo duplica: el índice único parcial de outbox_jobs lo garantiza.
func (r *outboxRepository) Enqueue(ctx context.Context, job domain.OutboxJob) error {
	const query = `
		INSERT INTO outbox_jobs (id, call_id, operation, attempts, next_attempt_at, created_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)
		ON CONFLICT (call_id, operation) WHERE completed_at IS NULL DO NOTHING`
	_, err := r.db.Exec(ctx, query, job.ID, string(job.CallID), string(job.Operation),
		job.Attempts, job.NextAttemptAt, job.CreatedAt)
	if err != nil {
		return fmt.Errorf("no se pudo encolar el trabajo pendiente: %w", err)
	}
	return nil
}

// ClaimDue devuelve los trabajos vencidos y los aplaza: así otro proceso no
// los reclama mientras se ejecutan.
func (r *outboxRepository) ClaimDue(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]domain.OutboxJob, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id::text, call_id::text, operation, attempts, next_attempt_at, completed_at, last_error, created_at
		FROM outbox_jobs
		WHERE completed_at IS NULL AND next_attempt_at <= $1
		ORDER BY next_attempt_at
		LIMIT $2
		FOR UPDATE SKIP LOCKED`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("no se pudieron reclamar los trabajos pendientes: %w", err)
	}
	jobs := make([]domain.OutboxJob, 0)
	for rows.Next() {
		var (
			job       domain.OutboxJob
			callID    string
			operation string
		)
		if err := rows.Scan(&job.ID, &callID, &operation, &job.Attempts, &job.NextAttemptAt,
			&job.CompletedAt, &job.LastError, &job.CreatedAt); err != nil {
			rows.Close()
			return nil, fmt.Errorf("no se pudo leer el trabajo pendiente: %w", err)
		}
		job.CallID = domain.CallID(callID)
		job.Operation = domain.JobOperation(operation)
		jobs = append(jobs, job)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("no se pudieron leer los trabajos pendientes: %w", err)
	}

	for _, job := range jobs {
		const query = `UPDATE outbox_jobs SET next_attempt_at = $2 WHERE id = $1::uuid`
		if _, err := r.db.Exec(ctx, query, job.ID, now.Add(lease)); err != nil {
			return nil, fmt.Errorf("no se pudo aplazar el trabajo pendiente: %w", err)
		}
	}
	return jobs, nil
}

func (r *outboxRepository) MarkCompleted(ctx context.Context, id string, now time.Time) error {
	const query = `UPDATE outbox_jobs SET completed_at = $2, last_error = NULL WHERE id = $1::uuid`
	if _, err := r.db.Exec(ctx, query, id, now); err != nil {
		return fmt.Errorf("no se pudo completar el trabajo pendiente: %w", err)
	}
	return nil
}

func (r *outboxRepository) MarkFailed(ctx context.Context, id string, attempts int, nextAttemptAt time.Time, lastError string) error {
	const query = `UPDATE outbox_jobs SET attempts = $2, next_attempt_at = $3, last_error = $4 WHERE id = $1::uuid`
	if _, err := r.db.Exec(ctx, query, id, attempts, nextAttemptAt, lastError); err != nil {
		return fmt.Errorf("no se pudo registrar el fallo del trabajo pendiente: %w", err)
	}
	return nil
}

func (r *outboxRepository) MarkAbandoned(ctx context.Context, id string, attempts int, now time.Time, lastError string) error {
	const query = `UPDATE outbox_jobs SET attempts = $2, completed_at = $3, last_error = $4 WHERE id = $1::uuid`
	if _, err := r.db.Exec(ctx, query, id, attempts, now, lastError); err != nil {
		return fmt.Errorf("no se pudo abandonar el trabajo pendiente: %w", err)
	}
	return nil
}
