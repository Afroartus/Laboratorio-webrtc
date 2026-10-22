package clock

import (
	"time"

	"webrtc/api/internal/application/ports"
)

// systemClock implementa el puerto Clock con el reloj del sistema.
type systemClock struct{}

// New devuelve el reloj del sistema.
func New() ports.Clock {
	return systemClock{}
}

// Now devuelve la hora actual en UTC.
func (systemClock) Now() time.Time {
	return time.Now().UTC()
}
