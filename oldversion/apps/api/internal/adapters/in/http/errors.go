package httpadapter

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"webrtc/api/internal/application/ports"
)

// Códigos de error del contrato. Se mantienen sincronizados con el enum
// ErrorCode de openapi.yaml (lo comprueba TestOpenAPIInSync).
const (
	codeValidation           = "VALIDATION_ERROR"
	codeInvalidCredentials   = "INVALID_CREDENTIALS"
	codeUnauthenticated      = "UNAUTHENTICATED"
	codeForbidden            = "FORBIDDEN"
	codeOriginNotAllowed     = "ORIGIN_NOT_ALLOWED"
	codeCSRFInvalid          = "CSRF_TOKEN_INVALID"
	codeNotFound             = "NOT_FOUND"
	codeCallNotFound         = "CALL_NOT_FOUND"
	codeMethodNotAllowed     = "METHOD_NOT_ALLOWED"
	codeCallStateConflict    = "CALL_STATE_CONFLICT"
	codeIdempotencyConflict  = "IDEMPOTENCY_KEY_CONFLICT"
	codeCallEnded            = "CALL_ENDED"
	codePayloadTooLarge      = "PAYLOAD_TOO_LARGE"
	codeUnsupportedMediaType = "UNSUPPORTED_MEDIA_TYPE"
	codeTooManyRequests      = "TOO_MANY_REQUESTS"
	codeInternal             = "INTERNAL_ERROR"
	codeUnavailable          = "SERVICE_UNAVAILABLE"
)

// errorCodes enumera todos los códigos que puede emitir la API.
var errorCodes = []string{
	codeValidation, codeInvalidCredentials, codeUnauthenticated, codeForbidden,
	codeOriginNotAllowed, codeCSRFInvalid, codeNotFound, codeCallNotFound,
	codeMethodNotAllowed, codeCallStateConflict, codeIdempotencyConflict,
	codeCallEnded, codePayloadTooLarge, codeUnsupportedMediaType,
	codeTooManyRequests, codeInternal, codeUnavailable,
}

// apiError es un error ya expresado en términos del contrato HTTP. Lo producen
// las validaciones del propio adaptador (formato, origen, CSRF).
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.code + ": " + e.message }

func newAPIError(status int, code, message string) *apiError {
	return &apiError{status: status, code: code, message: message}
}

func errValidation(message string) *apiError {
	return newAPIError(fiber.StatusBadRequest, codeValidation, message)
}

// applicationErrors traduce los errores de aplicación. El orden importa solo si
// un caso de uso envolviera varios a la vez; el primero que coincide gana.
var applicationErrors = []struct {
	target error
	resp   apiError
}{
	{ports.ErrInvalidInput, apiError{fiber.StatusBadRequest, codeValidation, "La solicitud no es válida"}},
	{ports.ErrInvalidCredentials, apiError{fiber.StatusUnauthorized, codeInvalidCredentials, "Usuario o contraseña incorrectos"}},
	{ports.ErrUnauthenticated, apiError{fiber.StatusUnauthorized, codeUnauthenticated, "La sesión no es válida"}},
	{ports.ErrForbidden, apiError{fiber.StatusForbidden, codeForbidden, "Operación no permitida"}},
	{ports.ErrCallNotFound, apiError{fiber.StatusNotFound, codeCallNotFound, "La llamada no existe"}},
	{ports.ErrCallStateConflict, apiError{fiber.StatusConflict, codeCallStateConflict, "La llamada no admite esta operación en su estado actual"}},
	{ports.ErrIdempotencyConflict, apiError{fiber.StatusConflict, codeIdempotencyConflict, "La clave de idempotencia ya se usó con otra solicitud"}},
	{ports.ErrCallEnded, apiError{fiber.StatusGone, codeCallEnded, "La llamada terminó"}},
	{ports.ErrTooManyAttempts, apiError{fiber.StatusTooManyRequests, codeTooManyRequests, "Demasiados intentos; espere antes de reintentar"}},
	{ports.ErrUnavailable, apiError{fiber.StatusServiceUnavailable, codeUnavailable, "Servicio no disponible temporalmente"}},
}

// errorResponse es el cuerpo de error común: { code, message, requestId }.
type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

// toAPIError clasifica cualquier error en una respuesta del contrato. Los
// mensajes son fijos: nunca se copia el texto de un error interno al cliente.
func toAPIError(err error) (resp apiError, known bool) {
	if ae, ok := errors.AsType[*apiError](err); ok {
		return *ae, true
	}
	for _, m := range applicationErrors {
		if errors.Is(err, m.target) {
			return m.resp, true
		}
	}
	if fe, ok := errors.AsType[*fiber.Error](err); ok {
		switch fe.Code {
		case fiber.StatusNotFound:
			return apiError{fe.Code, codeNotFound, "Recurso inexistente"}, true
		case fiber.StatusMethodNotAllowed:
			return apiError{fe.Code, codeMethodNotAllowed, "Método no permitido"}, true
		case fiber.StatusRequestEntityTooLarge:
			return apiError{fe.Code, codePayloadTooLarge, "El cuerpo de la solicitud es demasiado grande"}, true
		}
		if fe.Code >= 400 && fe.Code < 500 {
			return apiError{fiber.StatusBadRequest, codeValidation, "La solicitud no es válida"}, true
		}
	}
	return apiError{fiber.StatusInternalServerError, codeInternal, "Error interno"}, false
}

// errorHandler escribe la respuesta de error y registra los errores no
// clasificados. Se usa como fiber.Config.ErrorHandler y desde accessLog.
func errorHandler(logger *slog.Logger) fiber.ErrorHandler {
	return func(c fiber.Ctx, err error) error {
		if requestIDFrom(c) == "" {
			// Errores del servidor previos al enrutado (p. ej. cuerpo demasiado
			// grande) no pasan por el middleware requestID.
			assignRequestID(c)
		}
		resp, known := toAPIError(err)
		if !known || resp.status >= fiber.StatusInternalServerError {
			logger.Error("error al atender la solicitud",
				"request_id", requestIDFrom(c),
				"method", c.Method(),
				"route", routePath(c),
				"error", err,
			)
		}
		return c.Status(resp.status).JSON(errorResponse{
			Code:      resp.code,
			Message:   resp.message,
			RequestID: requestIDFrom(c),
		})
	}
}
