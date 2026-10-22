package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"webrtc/api/internal/application/ports"
)

// dbtx es el conjunto mínimo de operaciones de base de datos que comparten el
// pool (fuera de una transacción) y la transacción en curso.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store construye repositorios y la unidad de trabajo sobre el pool.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore devuelve el store del pool indicado.
func NewStore(pool *Pool) *Store {
	return &Store{pool: pool.pool}
}

// Repositories devuelve repositorios fuera de una transacción. Solo se usan
// para lectura; las escrituras pasan por la unidad de trabajo.
func (s *Store) Repositories() ports.Repositories {
	return newRepositories(s.pool)
}

// UnitOfWork devuelve la unidad de trabajo del pool.
func (s *Store) UnitOfWork() ports.UnitOfWork {
	return &unitOfWork{pool: s.pool}
}

type repositories struct {
	calls    *callRepository
	members  *callMemberRepository
	sessions *callSessionRepository
	events   *eventRepository
	outbox   *outboxRepository
	users    *userRepository
}

func newRepositories(db dbtx) *repositories {
	return &repositories{
		calls:    &callRepository{db: db},
		members:  &callMemberRepository{db: db},
		sessions: &callSessionRepository{db: db},
		events:   &eventRepository{db: db},
		outbox:   &outboxRepository{db: db},
		users:    &userRepository{db: db},
	}
}

func (r *repositories) Calls() ports.CallRepository           { return r.calls }
func (r *repositories) Members() ports.CallMemberRepository   { return r.members }
func (r *repositories) Sessions() ports.CallSessionRepository { return r.sessions }
func (r *repositories) Events() ports.EventRepository         { return r.events }
func (r *repositories) Outbox() ports.OutboxRepository        { return r.outbox }
func (r *repositories) Users() ports.UserRepository           { return r.users }

type unitOfWork struct {
	pool *pgxpool.Pool
}

// WithinTransaction ejecuta la función dentro de una transacción. Los
// repositorios que recibe quedan vinculados a ella: si la función devuelve un
// error, todo se revierte.
func (u *unitOfWork) WithinTransaction(ctx context.Context, fn func(ctx context.Context, repos ports.Repositories) error) error {
	tx, err := u.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("no se pudo iniciar la transacción: %w", err)
	}
	// Tras un commit esta llamada no hace nada (pgx devuelve ErrTxClosed).
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(ctx, newRepositories(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("no se pudo confirmar la transacción: %w", err)
	}
	return nil
}
