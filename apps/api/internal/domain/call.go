package domain

// Vocabulario de llamadas que comparten los contratos de aplicación. Las reglas
// de transición y de asignación de permisos por rol pertenecen a los casos de
// uso de los pasos 5 y 6; aquí solo se fijan los valores válidos.

// UserID identifica a un usuario local.
type UserID string

// CallID identifica una llamada (UUID).
type CallID string

// Role es el papel de un miembro dentro de una llamada.
type Role string

const (
	RoleCreator     Role = "creator"
	RoleParticipant Role = "participant"
	RoleViewer      Role = "viewer"
)

// CallStatus es el estado de negocio de una llamada:
// CREATED → ACTIVE → ENDING → ENDED, o CREATED → CANCELLED.
type CallStatus string

const (
	CallCreated   CallStatus = "CREATED"
	CallActive    CallStatus = "ACTIVE"
	CallEnding    CallStatus = "ENDING"
	CallEnded     CallStatus = "ENDED"
	CallCancelled CallStatus = "CANCELLED"
)

// Permissions describe qué puede hacer un miembro con los medios de la sala.
type Permissions struct {
	PublishCamera     bool
	PublishMicrophone bool
	PublishScreen     bool
	Subscribe         bool
}
