package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool agrupa el pool de conexiones a PostgreSQL.
type Pool struct {
	pool *pgxpool.Pool
}

// Open crea el pool de conexiones. No espera a que la base responda: eso lo
// hace WaitReady, para que el arranque pueda reintentar una base que todavía
// no está disponible.
func Open(ctx context.Context, dsn string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("DATABASE_URL inválida: %w", err)
	}
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	// El laboratorio trabaja siempre en UTC.
	cfg.ConnConfig.RuntimeParams["timezone"] = "UTC"
	if cfg.MaxConns <= 0 {
		cfg.MaxConns = 10
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("no se pudo crear el pool de PostgreSQL: %w", err)
	}
	return &Pool{pool: pool}, nil
}

// WaitReady espera a que PostgreSQL responda, reintentando con backoff.
func (p *Pool) WaitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	delay := 250 * time.Millisecond
	var lastErr error
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err := p.pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if !time.Now().Before(deadline) {
			return fmt.Errorf("PostgreSQL no respondió en %s: %w", timeout, lastErr)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		if delay < 5*time.Second {
			delay *= 2
		}
	}
}

// Close cierra el pool.
func (p *Pool) Close() {
	p.pool.Close()
}
