package httpadapter

import (
	"unicode/utf8"

	"webrtc/api/internal/application/ports"
	"webrtc/api/internal/domain"
)

// DTO del contrato HTTP. Son la única representación JSON de la API: los tipos
// de dominio y de aplicación no llevan etiquetas JSON.

const (
	maxUsernameLength       = 64
	maxPasswordLength       = 256
	maxUserIDLength         = 64
	maxIdempotencyKeyLength = 255
)

func (r loginRequest) validate() error {
	if n := utf8.RuneCountInString(r.Username); n == 0 || n > maxUsernameLength {
		return errValidation("username es obligatorio y admite hasta 64 caracteres")
	}
	if n := len(r.Password); n == 0 || n > maxPasswordLength {
		return errValidation("password es obligatorio y admite hasta 256 bytes")
	}
	return nil
}

func toUserResponse(u ports.UserSummary) userResponse {
	return userResponse{
		ID:          string(u.ID),
		Username:    u.Username,
		DisplayName: u.DisplayName,
	}
}

func toSessionResponse(s ports.Session) sessionResponse {
	return sessionResponse{
		User:      toUserResponse(s.User),
		CSRFToken: s.CSRFToken,
		ExpiresAt: s.ExpiresAt.UTC(),
	}
}

func (r createCallRequest) toInput(actor domain.UserID, idempotencyKey string) (ports.CreateCallInput, error) {
	if n := len(r.InviteeUserID); n == 0 || n > maxUserIDLength {
		return ports.CreateCallInput{}, errValidation("inviteeUserId es obligatorio")
	}
	role, ok := inviteeRoles[r.InviteeRole]
	if !ok {
		return ports.CreateCallInput{}, errValidation("inviteeRole debe ser participant o viewer")
	}
	return ports.CreateCallInput{
		ActorID:        actor,
		InviteeID:      domain.UserID(r.InviteeUserID),
		InviteeRole:    role,
		IdempotencyKey: idempotencyKey,
	}, nil
}

// validIdempotencyKey admite de 1 a 255 caracteres ASCII visibles.
func validIdempotencyKey(key string) bool {
	if len(key) == 0 || len(key) > maxIdempotencyKeyLength {
		return false
	}
	for i := range len(key) {
		if key[i] < 0x21 || key[i] > 0x7e {
			return false
		}
	}
	return true
}

func toPermissionsResponse(p domain.Permissions) permissionsResponse {
	return permissionsResponse{
		PublishCamera:     p.PublishCamera,
		PublishMicrophone: p.PublishMicrophone,
		PublishScreen:     p.PublishScreen,
		Subscribe:         p.Subscribe,
	}
}

func toCallResponse(v ports.CallView) callResponse {
	return callResponse{
		CallID:       string(v.CallID),
		Status:       string(v.Status),
		Role:         string(v.Role),
		Permissions:  toPermissionsResponse(v.Permissions),
		JoinDeadline: v.JoinDeadline.UTC(),
	}
}

func toJoinTokenResponse(j ports.JoinCredentials) joinTokenResponse {
	return joinTokenResponse{
		CallID:           string(j.CallID),
		RoomName:         j.RoomName,
		ServerURL:        j.ServerURL,
		ParticipantToken: j.ParticipantToken,
		ExpiresAt:        j.ExpiresAt.UTC(),
		Permissions:      toPermissionsResponse(j.Permissions),
	}
}

// parseCallID acepta solo UUID en forma canónica. Un identificador mal formado
// se trata como llamada no visible, igual que uno inexistente.
func parseCallID(raw string) (domain.CallID, error) {
	if len(raw) != 36 {
		return "", ports.ErrCallNotFound
	}
	buf := []byte(raw)
	for i, ch := range buf {
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if ch != '-' {
				return "", ports.ErrCallNotFound
			}
		case ch >= '0' && ch <= '9', ch >= 'a' && ch <= 'f':
		case ch >= 'A' && ch <= 'F':
			buf[i] = ch + ('a' - 'A')
		default:
			return "", ports.ErrCallNotFound
		}
	}
	return domain.CallID(buf), nil
}
