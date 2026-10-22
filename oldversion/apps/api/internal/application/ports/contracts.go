package ports

import (
	"context"
	"webrtc/api/internal/domain"
)

// Auth agrupa Login, Logout y la resolución de la sesión actual.
type Auth interface {
	Login(ctx context.Context, in LoginInput) (LoginResult, error)
	Logout(ctx context.Context, sessionToken string) error
	ResolveSession(ctx context.Context, sessionToken string) (Session, error)
}

// Users consulta el directorio de usuarios locales.
type Users interface {
	ListActiveUsers(ctx context.Context, actor domain.UserID) ([]UserSummary, error)
}

// Calls agrupa CreateCall, GetCall, IssueJoinToken y EndCall.
type Calls interface {
	CreateCall(ctx context.Context, in CreateCallInput) (CreateCallResult, error)
	GetCall(ctx context.Context, in CallAccess) (CallView, error)
	IssueJoinToken(ctx context.Context, in CallAccess) (JoinCredentials, error)
	EndCall(ctx context.Context, in CallAccess) (EndCallResult, error)
}
