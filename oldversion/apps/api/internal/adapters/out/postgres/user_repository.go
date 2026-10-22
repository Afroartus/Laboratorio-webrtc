package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

type userRepository struct {
	db dbtx
}

func (r *userRepository) GetByID(ctx context.Context, id domain.UserID) (domain.User, error) {
	const query = `
		SELECT id::text, username, display_name, password_hash, active, created_at
		FROM users WHERE id = $1::uuid`
	return r.get(ctx, query, string(id))
}

func (r *userRepository) GetByUsername(ctx context.Context, username string) (domain.User, error) {
	const query = `
		SELECT id::text, username, display_name, password_hash, active, created_at
		FROM users WHERE username = $1`
	return r.get(ctx, query, username)
}

func (r *userRepository) get(ctx context.Context, query string, arg any) (domain.User, error) {
	var (
		user  domain.User
		id    string
		shown string
	)
	err := r.db.QueryRow(ctx, query, arg).Scan(&id, &user.Username, &shown, &user.PasswordHash, &user.Active, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, ports.ErrInvalidInput
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("no se pudo leer el usuario: %w", err)
	}
	user.ID = domain.UserID(id)
	user.DisplayName = shown
	return user, nil
}
