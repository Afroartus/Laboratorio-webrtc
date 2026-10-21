package httpadapter

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"

	"webrtc/api/internal/application/ports"
)

const (
	sessionCookieName = "__Host-webrtc_session"
	csrfHeader        = "X-CSRF-Token"
	requestIDHeader   = "X-Request-ID"

	// maxSessionTokenLength descarta cookies manipuladas sin llegar al núcleo.
	maxSessionTokenLength = 256
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	authKey
)

// authContext es la sesión resuelta por requireSession para la solicitud actual.
type authContext struct {
	token   string
	session ports.Session
}

// requestID asigna un identificador propio a cada solicitud. No reutiliza el
// encabezado entrante para que un cliente no pueda inyectar valores en los logs.
func requestID(c fiber.Ctx) error {
	assignRequestID(c)
	return c.Next()
}

func assignRequestID(c fiber.Ctx) {
	id := rand.Text()
	c.Locals(requestIDKey, id)
	c.Set(requestIDHeader, id)
}

func requestIDFrom(c fiber.Ctx) string {
	id, _ := c.Locals(requestIDKey).(string)
	return id
}

func routePath(c fiber.Ctx) string {
	if r := c.Route(); r != nil {
		return r.Path
	}
	return ""
}

// accessLog resuelve los errores de la cadena antes de registrar la respuesta,
// para que el log refleje el estado HTTP real. Registra la plantilla de ruta,
// nunca cookies, encabezados de autorización ni cuerpos.
func accessLog(logger *slog.Logger, onError fiber.ErrorHandler) fiber.Handler {
	return func(c fiber.Ctx) error {
		start := time.Now()
		if err := c.Next(); err != nil {
			if herr := onError(c, err); herr != nil {
				return herr
			}
		}
		logger.Info("solicitud HTTP",
			"request_id", requestIDFrom(c),
			"method", c.Method(),
			"route", routePath(c),
			"status", c.Response().StatusCode(),
			"duration_ms", time.Since(start).Milliseconds(),
		)
		return nil
	}
}

// securityHeaders impide que intermediarios o el navegador almacenen
// respuestas de la API, que incluyen credenciales y datos de sesión.
func securityHeaders(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
	return c.Next()
}

// requireAllowedOrigin rechaza mutaciones cuyo Origin no coincide exactamente
// con un origen del laboratorio. Complementa a CSRF; no sustituye la
// autorización por usuario y llamada.
func requireAllowedOrigin(origins []string) fiber.Handler {
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[o] = struct{}{}
	}
	return func(c fiber.Ctx) error {
		if _, ok := allowed[c.Get(fiber.HeaderOrigin)]; !ok {
			return newAPIError(fiber.StatusForbidden, codeOriginNotAllowed, "Origen no permitido")
		}
		return c.Next()
	}
}

// requireSession resuelve la cookie de sesión mediante el puerto Auth.
func requireSession(auth ports.Auth) fiber.Handler {
	return func(c fiber.Ctx) error {
		token := c.Cookies(sessionCookieName)
		if token == "" || len(token) > maxSessionTokenLength {
			return ports.ErrUnauthenticated
		}
		session, err := auth.ResolveSession(c.Context(), token)
		if err != nil {
			return err
		}
		c.Locals(authKey, authContext{token: token, session: session})
		return c.Next()
	}
}

func authFrom(c fiber.Ctx) authContext {
	a, ok := c.Locals(authKey).(authContext)
	if !ok {
		// Solo ocurre si una ruta protegida se registra sin requireSession.
		panic("httpadapter: ruta protegida sin requireSession")
	}
	return a
}

// requireCSRF compara en tiempo constante el encabezado X-CSRF-Token con el
// token de la sesión. Debe ir después de requireSession.
func requireCSRF(c fiber.Ctx) error {
	expected := authFrom(c).session.CSRFToken
	got := c.Get(csrfHeader)
	if expected == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
		return newAPIError(fiber.StatusForbidden, codeCSRFInvalid, "Token CSRF ausente o inválido")
	}
	return c.Next()
}

// loginLimiter limita los intentos de login por IP de cliente (la que Caddy
// informa en X-Forwarded-For). Cuenta todos los intentos: el limitador de
// Fiber no distingue los errores de aplicación de las respuestas correctas.
// El bloqueo por cuenta corresponde al caso de uso (ports.ErrTooManyAttempts).
func loginLimiter(max int, window time.Duration) fiber.Handler {
	return limiter.New(limiter.Config{
		Max:          max,
		Expiration:   window,
		KeyGenerator: func(c fiber.Ctx) string { return c.IP() },
		LimitReached: func(fiber.Ctx) error {
			return newAPIError(fiber.StatusTooManyRequests, codeTooManyRequests, "Demasiados intentos; espere antes de reintentar")
		},
	})
}

// decodeJSON exige Content-Type application/json, rechaza campos desconocidos
// y contenido sobrante. Exigir JSON obliga a los navegadores a hacer preflight
// en peticiones de otro origen, lo que impide CSRF mediante formularios.
func decodeJSON(c fiber.Ctx, dst any) error {
	mediaType, _, err := mime.ParseMediaType(c.Get(fiber.HeaderContentType))
	if err != nil || mediaType != fiber.MIMEApplicationJSON {
		return newAPIError(fiber.StatusUnsupportedMediaType, codeUnsupportedMediaType, "Se requiere Content-Type: application/json")
	}
	dec := json.NewDecoder(bytes.NewReader(c.Body()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errValidation("El cuerpo JSON no es válido")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errValidation("El cuerpo JSON contiene datos adicionales")
	}
	return nil
}
