package domain

import (
	"fmt"
	"time"
)

// allowedTransitions define el ciclo de vida de una llamada:
// CREATED → ACTIVE → ENDING → ENDED, más la cancelación de una llamada vacía
// (CREATED → CANCELLED) y el cierre de una llamada que aún no se inició
// (CREATED → ENDING).
//
// ENDED y CANCELLED son terminales: ninguna transición sale de ellos, de modo
// que un error transitorio de infraestructura no reabra una llamada terminada.
var allowedTransitions = map[CallStatus][]CallStatus{
	CallCreated:   {CallActive, CallEnding, CallCancelled},
	CallActive:    {CallEnding},
	CallEnding:    {CallEnded},
	CallEnded:     {},
	CallCancelled: {},
}

// AllowedTransitions devuelve los estados alcanzables desde el estado indicado.
func AllowedTransitions(status CallStatus) []CallStatus {
	next := allowedTransitions[status]
	out := make([]CallStatus, len(next))
	copy(out, next)
	return out
}

// CanTransitionTo indica si la transición al estado indicado es válida.
func (c Call) CanTransitionTo(next CallStatus) bool {
	for _, allowed := range allowedTransitions[c.Status] {
		if allowed == next {
			return true
		}
	}
	return false
}

// TransitionTo cambia el estado de la llamada cuando la transición es válida.
// ACTIVE fija la hora de inicio; ENDED y CANCELLED fijan la de fin.
func (c *Call) TransitionTo(next CallStatus, now time.Time) error {
	if !c.CanTransitionTo(next) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, c.Status, next)
	}
	switch next {
	case CallActive:
		if c.StartedAt == nil {
			start := now
			c.StartedAt = &start
		}
	case CallEnded, CallCancelled:
		end := now
		c.EndedAt = &end
	}
	c.Status = next
	c.Version++
	return nil
}

// IsTerminal indica si la llamada ya no admite transiciones.
func (c Call) IsTerminal() bool {
	return c.Status == CallEnded || c.Status == CallCancelled
}

// DeadlineExceeded indica que se venció el plazo de ingreso de una llamada
// que nadie ha abierto todavía.
func (c Call) DeadlineExceeded(now time.Time) bool {
	return c.Status == CallCreated && now.After(c.JoinDeadline)
}

// DurationExceeded indica que la llamada activa superó su duración máxima.
func (c Call) DurationExceeded(now time.Time) bool {
	if c.Status != CallActive || c.StartedAt == nil {
		return false
	}
	limit := c.StartedAt.Add(time.Duration(c.MaxDurationSeconds) * time.Second)
	return now.After(limit)
}
