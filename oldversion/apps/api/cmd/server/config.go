package main

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// minLiveKitSecretLength es la longitud mínima que LiveKit acepta para un secreto de API.
const minLiveKitSecretLength = 32

// Valores por defecto de las variables operativas del ciclo de vida.
const (
	// defaultJoinDeadlineSeconds es el plazo para unirse a una llamada vacía.
	defaultJoinDeadlineSeconds = 600
	// defaultWorkerIntervalSeconds es la frecuencia del proceso periódico.
	defaultWorkerIntervalSeconds = 15
	// defaultOutboxMaxAttempts es el número de intentos de un trabajo pendiente.
	defaultOutboxMaxAttempts = 10
	// defaultAuditRetentionDays es la retención de la auditoría técnica.
	defaultAuditRetentionDays = 30
	// defaultDBConnectTimeoutSeconds es la espera máxima por una base disponible.
	defaultDBConnectTimeoutSeconds = 30
)

// config es el contrato de variables de entorno del servicio (paso 4 del spec).
// Contiene secretos: no registrar la estructura completa en logs.
type config struct {
	HTTPAddr          string
	AppBaseURL        *url.URL
	AllowedWebOrigins []string
	DatabaseURL       string
	LiveKitURL        *url.URL
	LiveKitAPIURL     *url.URL
	LiveKitAPIKey     string
	LiveKitAPISecret  string
	LabHostIP         netip.Addr
	AuthMode          string
	CallMaxDuration   time.Duration
	JoinTokenTTL      time.Duration
	JoinDeadline      time.Duration
	WorkerInterval    time.Duration
	OutboxMaxAttempts int
	AuditRetentionDay int
	DBConnectTimeout  time.Duration
}

// loadConfig lee y valida la configuración. Devuelve todos los problemas
// encontrados a la vez para que el arranque falle con un mensaje completo.
func loadConfig(getenv func(string) string) (config, error) {
	var cfg config
	var errs []error

	required := func(name string) string {
		v := strings.TrimSpace(getenv(name))
		if v == "" {
			errs = append(errs, fmt.Errorf("%s es obligatoria", name))
		}
		return v
	}

	parseURL := func(name string, schemes ...string) *url.URL {
		raw := required(name)
		if raw == "" {
			return nil
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || !slices.Contains(schemes, u.Scheme) {
			errs = append(errs, fmt.Errorf("%s debe ser una URL absoluta con esquema %s", name, strings.Join(schemes, " o ")))
			return nil
		}
		return u
	}
	positiveSeconds := func(name string) time.Duration {
		raw := required(name)
		if raw == "" {
			return 0
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("%s debe ser un entero positivo de segundos", name))
			return 0
		}
		return time.Duration(n) * time.Second
	}
	// secondsWithDefault usa un valor por defecto si la variable no está
	// definida. Solo se usa en variables operativas con valor razonable.
	secondsWithDefault := func(name string, fallback int) time.Duration {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return time.Duration(fallback) * time.Second
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("%s debe ser un entero positivo de segundos", name))
			return 0
		}
		return time.Duration(n) * time.Second
	}
	positiveIntWithDefault := func(name string, fallback int) int {
		raw := strings.TrimSpace(getenv(name))
		if raw == "" {
			return fallback
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("%s debe ser un entero positivo", name))
			return fallback
		}
		return n
	}

	cfg.HTTPAddr = strings.TrimSpace(getenv("HTTP_ADDR"))
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = ":8080"
	}

	cfg.AppBaseURL = parseURL("APP_BASE_URL", "https")

	for _, origin := range strings.FieldsFunc(required("ALLOWED_WEB_ORIGINS"), func(r rune) bool {
		return r == ',' || r == ' '
	}) {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
			errs = append(errs, fmt.Errorf("ALLOWED_WEB_ORIGINS contiene un origen inválido %q (formato https://host[:puerto])", origin))
			continue
		}
		cfg.AllowedWebOrigins = append(cfg.AllowedWebOrigins, u.Scheme+"://"+u.Host)
	}

	if raw := required("DATABASE_URL"); raw != "" {
		if u, err := url.Parse(raw); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			errs = append(errs, errors.New("DATABASE_URL debe ser una URL postgres://"))
		} else {
			cfg.DatabaseURL = raw
		}
	}

	cfg.LiveKitURL = parseURL("LIVEKIT_URL", "wss")
	cfg.LiveKitAPIURL = parseURL("LIVEKIT_API_URL", "http", "https")
	cfg.LiveKitAPIKey = required("LIVEKIT_API_KEY")
	cfg.LiveKitAPISecret = required("LIVEKIT_API_SECRET")
	if cfg.LiveKitAPISecret != "" && len(cfg.LiveKitAPISecret) < minLiveKitSecretLength {
		errs = append(errs, fmt.Errorf("LIVEKIT_API_SECRET debe tener al menos %d caracteres", minLiveKitSecretLength))
	}

	if raw := required("LAB_HOST_IP"); raw != "" {
		ip, err := netip.ParseAddr(raw)
		if err != nil || ip.IsUnspecified() {
			errs = append(errs, errors.New("LAB_HOST_IP debe ser una IP concreta alcanzable por los navegadores"))
		} else {
			cfg.LabHostIP = ip
		}
	}

	cfg.AuthMode = required("AUTH_MODE")
	if cfg.AuthMode != "" && cfg.AuthMode != "local" {
		errs = append(errs, fmt.Errorf("AUTH_MODE=%q no soportado; el laboratorio solo admite \"local\"", cfg.AuthMode))
	}

	cfg.CallMaxDuration = positiveSeconds("CALL_MAX_DURATION_SECONDS")
	cfg.JoinTokenTTL = positiveSeconds("JOIN_TOKEN_TTL_SECONDS")

	// Variables del ciclo de vida de las llamadas (paso 6).
	cfg.JoinDeadline = secondsWithDefault("JOIN_DEADLINE_SECONDS", defaultJoinDeadlineSeconds)
	cfg.WorkerInterval = secondsWithDefault("WORKER_INTERVAL_SECONDS", defaultWorkerIntervalSeconds)
	cfg.OutboxMaxAttempts = positiveIntWithDefault("OUTBOX_MAX_ATTEMPTS", defaultOutboxMaxAttempts)
	cfg.AuditRetentionDay = positiveIntWithDefault("AUDIT_RETENTION_DAYS", defaultAuditRetentionDays)
	cfg.DBConnectTimeout = secondsWithDefault("DB_CONNECT_TIMEOUT_SECONDS", defaultDBConnectTimeoutSeconds)

	return cfg, errors.Join(errs...)
}
