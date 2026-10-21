package httpadapter

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/recover"

	"webrtc/api/internal/application/ports"
)

const (
	defaultBodyLimit       = 64 * 1024
	defaultLoginRateLimit  = 10
	defaultLoginRateWindow = time.Minute
)

// Config es la configuración del contrato HTTP.
type Config struct {
	// AppBaseURL es la URL pública de la web; con ella se construye joinUrl.
	AppBaseURL *url.URL
	// AllowedOrigins son los orígenes exactos admitidos en mutaciones.
	AllowedOrigins []string
	// LoginRateLimit intentos de login por IP en cada LoginRateWindow
	// (por defecto 10 por minuto).
	LoginRateLimit  int
	LoginRateWindow time.Duration
}

// WebhookReceiver es lo que el endpoint de webhooks necesita del adaptador de
// eventos: verificar la firma sobre el cuerpo original, traducir el evento y
// persistirlo. Devuelve ports.ErrUnauthenticated si la firma no es válida.
type WebhookReceiver interface {
	Receive(ctx context.Context, body []byte, authorization string) error
}

// Dependencies son los puertos de entrada que invocan los handlers.
type Dependencies struct {
	Auth     ports.Auth
	Users    ports.Users
	Calls    ports.Calls
	Webhooks WebhookReceiver
}

// NewApp crea la aplicación Fiber con el manejo común: identificador de
// solicitud, log de acceso, recuperación de pánicos, encabezados de seguridad
// y formato de error del contrato.
func NewApp(logger *slog.Logger) *fiber.App {
	onError := errorHandler(logger)
	app := fiber.New(fiber.Config{
		ErrorHandler: onError,
		// Los valores del contexto se copian: los casos de uso pueden
		// conservarlos más allá del handler sin leer búferes reutilizados.
		Immutable:    true,
		BodyLimit:    defaultBodyLimit,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
		// Caddy, en la red interna de Podman, reemplaza X-Forwarded-For con la
		// IP real del cliente. Solo se confía en ese encabezado si la conexión
		// llega desde una red privada o loopback.
		TrustProxy:       true,
		TrustProxyConfig: fiber.TrustProxyConfig{Loopback: true, Private: true},
		ProxyHeader:      fiber.HeaderXForwardedFor,
	})
	app.Use(requestID, accessLog(logger, onError), recover.New(), securityHeaders)
	return app
}

// RegisterAPI registra los endpoints /api/v1 del contrato OpenAPI.
func RegisterAPI(app *fiber.App, cfg Config, deps Dependencies) error {
	if cfg.AppBaseURL == nil || len(cfg.AllowedOrigins) == 0 {
		return errors.New("httpadapter: AppBaseURL y AllowedOrigins son obligatorios")
	}
	if deps.Auth == nil || deps.Users == nil || deps.Calls == nil || deps.Webhooks == nil {
		return errors.New("httpadapter: faltan dependencias de la API")
	}
	if cfg.LoginRateLimit <= 0 {
		cfg.LoginRateLimit = defaultLoginRateLimit
	}
	if cfg.LoginRateWindow <= 0 {
		cfg.LoginRateWindow = defaultLoginRateWindow
	}

	h := &handlers{
		auth:       deps.Auth,
		users:      deps.Users,
		calls:      deps.Calls,
		webhooks:   deps.Webhooks,
		appBaseURL: cfg.AppBaseURL,
	}
	origin := requireAllowedOrigin(cfg.AllowedOrigins)
	session := requireSession(deps.Auth)

	// Cada ruta declara su cadena completa. Las mutaciones del navegador exigen
	// origen permitido, sesión y CSRF; login no tiene sesión y se protege con
	// origen, Content-Type JSON y límite de intentos.
	app.Post("/api/v1/auth/login", origin, loginLimiter(cfg.LoginRateLimit, cfg.LoginRateWindow), h.login)
	app.Post("/api/v1/auth/logout", origin, session, requireCSRF, h.logout)
	app.Get("/api/v1/auth/me", session, h.me)
	app.Get("/api/v1/users", session, h.listUsers)
	app.Post("/api/v1/calls", origin, session, requireCSRF, h.createCall)
	app.Get("/api/v1/calls/:id", session, h.getCall)
	app.Post("/api/v1/calls/:id/join-token", origin, session, requireCSRF, h.issueJoinToken)
	app.Post("/api/v1/calls/:id/end", origin, session, requireCSRF, h.endCall)
	// Autenticado por la firma del proveedor; no usa cookie ni Origin.
	app.Post("/api/v1/webhooks/livekit", h.receiveWebhook)
	return nil
}
