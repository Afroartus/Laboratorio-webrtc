package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

type callMemberRepository struct {
	db dbtx
}

func (r *callMemberRepository) InsertMembers(ctx context.Context, callID domain.CallID, members []domain.CallMember) error {
	const query = `
		INSERT INTO call_members (call_id, user_id, role, allowed_sources, revoked_at, created_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)`
	for _, member := range members {
		sources := make([]string, 0, len(member.AllowedSources))
		for _, source := range member.AllowedSources {
			sources = append(sources, string(source))
		}
		_, err := r.db.Exec(ctx, query, string(callID), string(member.UserID), string(member.Role),
			sources, member.RevokedAt, member.CreatedAt)
		if err != nil {
			return fmt.Errorf("no se pudo guardar el miembro de la llamada: %w", err)
		}
	}
	return nil
}

func (r *callMemberRepository) ListByCall(ctx context.Context, callID domain.CallID) ([]domain.CallMember, error) {
	rows, err := r.db.Query(ctx, `
		SELECT user_id::text, role, allowed_sources, revoked_at, created_at
		FROM call_members WHERE call_id = $1::uuid ORDER BY created_at`, string(callID))
	if err != nil {
		return nil, fmt.Errorf("no se pudieron listar los miembros de la llamada: %w", err)
	}
	defer rows.Close()
	members := make([]domain.CallMember, 0)
	for rows.Next() {
		var (
			member  domain.CallMember
			userID  string
			role    string
			sources []string
		)
		if err := rows.Scan(&userID, &role, &sources, &member.RevokedAt, &member.CreatedAt); err != nil {
			return nil, fmt.Errorf("no se pudo leer el miembro de la llamada: %w", err)
		}
		member.UserID = domain.UserID(userID)
		member.Role = domain.Role(role)
		member.AllowedSources = make([]domain.MediaSource, 0, len(sources))
		for _, source := range sources {
			member.AllowedSources = append(member.AllowedSources, domain.MediaSource(source))
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("no se pudieron leer los miembros de la llamada: %w", err)
	}
	return members, nil
}

func (r *callMemberRepository) Get(ctx context.Context, callID domain.CallID, userID domain.UserID) (domain.CallMember, error) {
	var (
		member  domain.CallMember
		id      string
		role    string
		sources []string
	)
	err := r.db.QueryRow(ctx, `
		SELECT user_id::text, role, allowed_sources, revoked_at, created_at
		FROM call_members WHERE call_id = $1::uuid AND user_id = $2::uuid`,
		string(callID), string(userID)).Scan(&id, &role, &sources, &member.RevokedAt, &member.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.CallMember{}, ports.ErrCallNotFound
	}
	if err != nil {
		return domain.CallMember{}, fmt.Errorf("no se pudo leer el miembro de la llamada: %w", err)
	}
	member.UserID = domain.UserID(id)
	member.Role = domain.Role(role)
	member.AllowedSources = make([]domain.MediaSource, 0, len(sources))
	for _, source := range sources {
		member.AllowedSources = append(member.AllowedSources, domain.MediaSource(source))
	}
	return member, nil
}
