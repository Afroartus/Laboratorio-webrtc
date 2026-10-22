package domain

import "time"

// JobOperation es un efecto externo pendiente sobre el servidor de medios.
type JobOperation string

const (
	// OperationDeleteRoom elimina la sala de la llamada en el servidor de medios.
	OperationDeleteRoom JobOperation = "delete_room"
)

const (
	// baseBackoff es el retraso del primer reintento de un trabajo pendiente.
	baseBackoff = 5 * time.Second
	// maxBackoff acota el retraso entre reintentos.
	maxBackoff = 5 * time.Minute
)

// OutboxJob es un trabajo pendiente que se reintenta de forma idempotente.
// Se guarda en la misma transacción que el cambio de estado que lo originó.
type OutboxJob struct {
	ID            string
	CallID        CallID
	Operation     JobOperation
	Attempts      int
	NextAttemptAt time.Time
	CompletedAt   *time.Time
	LastError     string
	CreatedAt     time.Time
}

// IsDue indica si el trabajo puede intentarse en el instante indicado.
func (j OutboxJob) IsDue(now time.Time) bool {
	if j.CompletedAt != nil {
		return false
	}
	return !now.Before(j.NextAttemptAt)
}

// Backoff devuelve el retraso hasta el siguiente intento de un trabajo que ya
// falló el número de intentos indicado. Crece de forma exponencial y está
// acotado para no retrasar el cierre indefinidamente.
func Backoff(attempts int) time.Duration {
	if attempts < 0 {
		attempts = 0
	}
	delay := baseBackoff
	for i := 0; i < attempts; i++ {
		delay *= 2
		if delay >= maxBackoff {
			return maxBackoff
		}
	}
	return delay
}
