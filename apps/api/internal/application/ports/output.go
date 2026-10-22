package ports

import (
	"context"
	"errors"
	"time"

	"webrtc/api/internal/domain"
)

// ErrRoomNotFound indica que la sala no existe en el servidor de medios.
var ErrRoomNotFound = errors.New("sala inexistente")

// Clock entrega la hora actual. Lo implementa el adaptador de reloj del sistema
// y permite fijar el tiempo en las pruebas.
type Clock interface {
	Now() time.Time
}

// Repositories agrupa los repositorios disponibles. Los que entrega una
// transacción están vinculados a ella: al confirmarla se publican y al
// revertarla se descartan.
type Repositories interface {
	Calls() CallRepository
	Members() CallMemberRepository
	Sessions() CallSessionRepository
	Events() EventRepository
	Outbox() OutboxRepository
	Users() UserRepository
}

// UnitOfWork ejecuta una función dentro de una transacción: confirma si la
// función termina sin error y revierte en caso contrario.
type UnitOfWork interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context, repos Repositories) error) error
}

// CallRepository persiste las llamadas.
type CallRepository interface {
	// Insert guarda una llamada nueva.
	Insert(ctx context.Context, call domain.Call) error
	// GetByID devuelve la llamada o ports.ErrCallNotFound.
	GetByID(ctx context.Context, id domain.CallID) (domain.Call, error)
	// GetByIDForUpdate bloquea la fila de la llamada hasta el fin de la
	// transacción: serializa la emisión de credenciales y el cierre.
	GetByIDForUpdate(ctx context.Context, id domain.CallID) (domain.Call, error)
	// UpdateStatus cambia el estado comprobando la versión esperada. Si otra
	// transacción cambió la llamada primero devuelve ports.ErrCallStateConflict.
	UpdateStatus(ctx context.Context, id domain.CallID, expectedVersion int, from, to domain.CallStatus, now time.Time) (int, error)
	// ListByStatus devuelve las llamadas en el estado indicado.
	ListByStatus(ctx context.Context, status domain.CallStatus, limit int) ([]domain.Call, error)
	// ListJoinDeadlineExceeded devuelve las llamadas vacías cuyo plazo de
	// ingreso venció.
	ListJoinDeadlineExceeded(ctx context.Context, now time.Time, limit int) ([]domain.Call, error)
	// ListDurationExceeded devuelve las llamadas activas que superaron su
	// duración máxima.
	ListDurationExceeded(ctx context.Context, now time.Time, limit int) ([]domain.Call, error)
}

// CallMemberRepository persiste la pertenencia a las llamadas.
type CallMemberRepository interface {
	// InsertMembers guarda los miembros de una llamada.
	InsertMembers(ctx context.Context, callID domain.CallID, members []domain.CallMember) error
	// ListByCall devuelve los miembros de la llamada.
	ListByCall(ctx context.Context, callID domain.CallID) ([]domain.CallMember, error)
	// Get devuelve la pertenencia del usuario o ports.ErrCallNotFound.
	Get(ctx context.Context, callID domain.CallID, userID domain.UserID) (domain.CallMember, error)
}

// CallSessionRepository persiste las conexiones de los participantes.
type CallSessionRepository interface {
	// Insert registra una conexión de un participante.
	Insert(ctx context.Context, session domain.CallSession) error
	// Close marca la conexión como finalizada.
	Close(ctx context.Context, id string, at time.Time, reason string) error
	// ListOpenByCall devuelve las conexiones sin cerrar de la llamada.
	ListOpenByCall(ctx context.Context, callID domain.CallID) ([]domain.CallSession, error)
}

// EventRepository persiste la auditoría de las llamadas.
type EventRepository interface {
	// Append guarda un evento.
	Append(ctx context.Context, event domain.CallEvent) error
	// AppendProviderEvent guarda un evento procedente del proveedor. Si el
	// identificador del proveedor ya existe devuelve false y no duplica nada.
	AppendProviderEvent(ctx context.Context, event domain.CallEvent) (bool, error)
}

// OutboxRepository persiste los trabajos pendientes de efecto externo.
type OutboxRepository interface {
	// Enqueue guarda un trabajo pendiente. Si ya existe uno igual sin
	// completar no lo duplica.
	Enqueue(ctx context.Context, job domain.OutboxJob) error
	// ClaimDue devuelve los trabajos vencidos y los aplaza para que otro
	// proceso no los reclame mientras se ejecutan.
	ClaimDue(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]domain.OutboxJob, error)
	// MarkCompleted marca el trabajo como terminado.
	MarkCompleted(ctx context.Context, id string, now time.Time) error
	// MarkFailed registra el error y programa el siguiente intento.
	MarkFailed(ctx context.Context, id string, attempts int, nextAttemptAt time.Time, lastError string) error
	// MarkAbandoned cierra el trabajo tras agotar los reintentos y conserva el
	// error como rastro.
	MarkAbandoned(ctx context.Context, id string, attempts int, now time.Time, lastError string) error
}

// UserRepository consulta el directorio de usuarios locales.
type UserRepository interface {
	// GetByID devuelve el usuario o ports.ErrInvalidInput si no existe.
	GetByID(ctx context.Context, id domain.UserID) (domain.User, error)
	// GetByUsername devuelve el usuario o ports.ErrInvalidInput si no existe.
	GetByUsername(ctx context.Context, username string) (domain.User, error)
}

// RoomService opera sobre las salas del servidor de medios.
type RoomService interface {
	// GetRoom devuelve la información de la sala o ports.ErrRoomNotFound.
	GetRoom(ctx context.Context, roomName string) (RoomInfo, error)
	// DeleteRoom elimina la sala. Es idempotente: si la sala no existe no
	// devuelve error.
	DeleteRoom(ctx context.Context, roomName string) error
}

// RoomInfo describe una sala del servidor de medios.
type RoomInfo struct {
	Name         string
	SID          string
	Participants int
}
