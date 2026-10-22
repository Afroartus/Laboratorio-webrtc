package httpadapter

import (
	"time"
	"webrtc/api/internal/domain"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type userResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

type sessionResponse struct {
	User      userResponse `json:"user"`
	CSRFToken string       `json:"csrfToken"`
	ExpiresAt time.Time    `json:"expiresAt"`
}

type createCallRequest struct {
	InviteeUserID string `json:"inviteeUserId"`
	InviteeRole   string `json:"inviteeRole"`
}

// inviteeRoles son los roles que el contrato admite para el invitado.
var inviteeRoles = map[string]domain.Role{
	string(domain.RoleParticipant): domain.RoleParticipant,
	string(domain.RoleViewer):      domain.RoleViewer,
}

type usersResponse struct {
	Users []userResponse `json:"users"`
}

type createCallResponse struct {
	CallID  string `json:"callId"`
	JoinURL string `json:"joinUrl"`
	Status  string `json:"status"`
}

type permissionsResponse struct {
	PublishCamera     bool `json:"publishCamera"`
	PublishMicrophone bool `json:"publishMicrophone"`
	PublishScreen     bool `json:"publishScreen"`
	Subscribe         bool `json:"subscribe"`
}

type callResponse struct {
	CallID       string              `json:"callId"`
	Status       string              `json:"status"`
	Role         string              `json:"role"`
	Permissions  permissionsResponse `json:"permissions"`
	JoinDeadline time.Time           `json:"joinDeadline"`
}

type joinTokenResponse struct {
	CallID           string              `json:"callId"`
	RoomName         string              `json:"roomName"`
	ServerURL        string              `json:"serverUrl"`
	ParticipantToken string              `json:"participantToken"`
	ExpiresAt        time.Time           `json:"expiresAt"`
	Permissions      permissionsResponse `json:"permissions"`
}

type endCallResponse struct {
	Status string `json:"status"`
}
