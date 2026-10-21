package ports

import (
	"time"

	"webrtc/api/internal/domain"
)

// Puertos de entrada: lo que los adaptadores de entrada pueden pedir al núcleo.
// Los casos de uso los implementan; los handlers dependen solo de estas
// interfaces.

// UserSummary es la información pública de un usuario local.
type UserSummary struct {
	ID          domain.UserID
	Username    string
	DisplayName string
}

// Session es una sesión de usuario válida.
type Session struct {
	User      UserSummary
	CSRFToken string
	ExpiresAt time.Time
}

// LoginInput son las credenciales presentadas por el usuario.
type LoginInput struct {
	Username string
	Password string
}

// LoginResult contiene el identificador opaco de sesión que el adaptador
// entrega al cliente. Solo su hash se persiste.
type LoginResult struct {
	SessionToken string
	Session      Session
}

// CreateCallInput crea una llamada con el actor como creador y un invitado.
type CreateCallInput struct {
	ActorID     domain.UserID
	InviteeID   domain.UserID
	InviteeRole domain.Role
	// IdempotencyKey es opcional. Con la misma clave y el mismo actor, una
	// solicitud equivalente devuelve la llamada original; una distinta,
	// ErrIdempotencyConflict.
	IdempotencyKey string
}

// CreateCallResult describe la llamada creada o recuperada por idempotencia.
type CreateCallResult struct {
	CallID   domain.CallID
	Status   domain.CallStatus
	Replayed bool
}

// CallAccess identifica al actor y la llamada sobre la que opera.
type CallAccess struct {
	ActorID domain.UserID
	CallID  domain.CallID
}

// CallView es la llamada vista por uno de sus miembros.
type CallView struct {
	CallID       domain.CallID
	Status       domain.CallStatus
	Role         domain.Role
	Permissions  domain.Permissions
	JoinDeadline time.Time
}

// JoinCredentials son las credenciales temporales para conectar con el
// servidor de medios.
type JoinCredentials struct {
	CallID           domain.CallID
	RoomName         string
	ServerURL        string
	ParticipantToken string
	ExpiresAt        time.Time
	Permissions      domain.Permissions
}

// EndCallResult es el estado de la llamada tras solicitar su cierre.
type EndCallResult struct {
	CallID domain.CallID
	Status domain.CallStatus
}
