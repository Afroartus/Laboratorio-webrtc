package domain

import "time"

// MediaSource es una fuente de medios que un miembro puede publicar.
type MediaSource string

const (
	SourceCamera     MediaSource = "camera"
	SourceMicrophone MediaSource = "microphone"
	SourceScreen     MediaSource = "screen"
)

// CallMember es la pertenencia de un usuario a una llamada.
type CallMember struct {
	UserID         UserID
	Role           Role
	AllowedSources []MediaSource
	RevokedAt      *time.Time
	CreatedAt      time.Time
}

// CanPublish indica si el miembro puede publicar la fuente indicada.
func (m CallMember) CanPublish(source MediaSource) bool {
	if m.RevokedAt != nil {
		return false
	}
	for _, allowed := range m.AllowedSources {
		if allowed == source {
			return true
		}
	}
	return false
}

// Call es la entidad de una llamada con su estado de negocio.
type Call struct {
	ID                 CallID
	RoomName           string
	OwnerID            UserID
	Status             CallStatus
	CreatedAt          time.Time
	JoinDeadline       time.Time
	StartedAt          *time.Time
	EndedAt            *time.Time
	MaxDurationSeconds int
	Version            int
	Members            []CallMember
}

// Member devuelve la pertenencia del usuario indicado.
func (c Call) Member(userID UserID) (CallMember, bool) {
	for _, member := range c.Members {
		if member.UserID == userID {
			return member, true
		}
	}
	return CallMember{}, false
}

// CallSession es la conexión de un participante con la sala de una llamada.
type CallSession struct {
	ID               string
	CallID           CallID
	UserID           UserID
	ParticipantSID   string
	RoomSID          string
	JoinedAt         time.Time
	LeftAt           *time.Time
	DisconnectReason string
}

// User es un usuario local del laboratorio.
type User struct {
	ID           UserID
	Username     string
	DisplayName  string
	PasswordHash string
	Active       bool
	CreatedAt    time.Time
}

// CallEvent es un hecho registrado en la auditoría de una llamada.
type CallEvent struct {
	ID              string
	CallID          CallID
	EventType       string
	ActorID         *UserID
	OccurredAt      time.Time
	ProviderEventID string
	Details         map[string]any
}
