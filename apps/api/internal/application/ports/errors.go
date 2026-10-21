package ports

import "errors"

// Errores de aplicación. Los casos de uso los devuelven (o los envuelven con
// fmt.Errorf("...: %w", ErrX)) y los adaptadores de entrada los traducen a su
// protocolo. Ningún adaptador debe inspeccionar errores técnicos (SQL, SDK).

var (
	// ErrInvalidInput: datos con formato válido pero inaceptables (p. ej. un
	// invitado inexistente o el propio creador).
	ErrInvalidInput = errors.New("entrada inválida")
	// ErrInvalidCredentials: usuario o contraseña incorrectos, o usuario inactivo.
	ErrInvalidCredentials = errors.New("credenciales inválidas")
	// ErrUnauthenticated: sesión ausente, vencida o revocada, o firma de un
	// proveedor inválida.
	ErrUnauthenticated = errors.New("no autenticado")
	// ErrForbidden: el actor es visible pero no puede realizar la operación.
	ErrForbidden = errors.New("operación no permitida")
	// ErrCallNotFound: la llamada no existe o el actor no es miembro.
	ErrCallNotFound = errors.New("llamada inexistente")
	// ErrCallStateConflict: la operación no es válida en el estado actual.
	ErrCallStateConflict = errors.New("conflicto de estado de la llamada")
	// ErrIdempotencyConflict: la clave de idempotencia ya se usó con otra solicitud.
	ErrIdempotencyConflict = errors.New("clave de idempotencia reutilizada")
	// ErrCallEnded: la llamada terminó (ENDING, ENDED o CANCELLED) para un
	// miembro autorizado.
	ErrCallEnded = errors.New("la llamada terminó")
	// ErrTooManyAttempts: se superó el límite de intentos.
	ErrTooManyAttempts = errors.New("demasiados intentos")
	// ErrUnavailable: una dependencia (base de datos, servidor de medios) no
	// está disponible.
	ErrUnavailable = errors.New("dependencia no disponible")
)

// const (
// 	ErrInvalidInput        errorPort = "ErrInvalidInput"
// 	ErrInvalidCredentials  errorPort = "ErrInvalidCredentials"
// 	ErrUnauthenticated     errorPort = "ErrUnauthenticated"
// 	ErrForbidden           errorPort = "ErrForbidden"
// 	ErrCallNotFound        errorPort = "ErrCallNotFound"
// 	ErrCallStateConflict   errorPort = "ErrCallStateConflict"
// 	ErrIdempotencyConflict errorPort = "ErrIdempotencyConflict"
// 	ErrCallEnded           errorPort = "ErrCallEnded"
// 	ErrTooManyAttempts     errorPort = "ErrTooManyAttempts"
// 	ErrUnavailable         errorPort = "ErrUnavailable"
// )

// var (
// 	ErrorMap = map[errorPort]error{
// 		ErrInvalidInput:        errors.New("entrada inválida"),
// 		ErrInvalidCredentials:  errors.New("credenciales inválidas"),
// 		ErrUnauthenticated:     errors.New("no autenticado"),
// 		ErrForbidden:           errors.New("operación no permitida"),
// 		ErrCallNotFound:        errors.New("llamada inexistente"),
// 		ErrCallStateConflict:   errors.New("conflicto de estado de la llamada"),
// 		ErrIdempotencyConflict: errors.New("clave de idempotencia reutilizada"),
// 		ErrCallEnded:           errors.New("la llamada terminó"),
// 		ErrTooManyAttempts:     errors.New("demasiados intentos"),
// 		ErrUnavailable:         errors.New("dependencia no disponible"),
// 	}
