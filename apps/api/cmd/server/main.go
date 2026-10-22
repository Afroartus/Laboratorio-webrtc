// Command server es el punto de composición del backend: carga la
// configuración, ensambla adaptadores y casos de uso, registra handlers y
// gestiona el ciclo de vida del proceso.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"

	httpadapter "webrtc/api/internal/adapters/in/http"
	"webrtc/api/internal/adapters/in/jobs"
	"webrtc/api/internal/adapters/out/clock"
	"webrtc/api/internal/adapters/out/livekit"
	"webrtc/api/internal/adapters/out/postgres"
	"webrtc/api/internal/application/usecases"
)

const shutdownTimeout = 10 * time.Second

func main() {
	healthcheck := flag.Bool("healthcheck", false, "consulta /health/live del proceso local y termina (para la sonda del contenedor)")
	flag.Parse()

	if *healthcheck {
		os.Exit(runHealthcheck(os.Getenv("HTTP_ADDR")))
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "configuración inválida:\n%v\n", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg, logger); err != nil {
		logger.Error("el servicio terminó con error", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config, logger *slog.Logger) error {
	// Base de datos: el servicio reintenta la conexión porque el arranque de
	// los contenedores no garantiza que PostgreSQL esté listo.
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.WaitReady(ctx, cfg.DBConnectTimeout); err != nil {
		return err
	}

	store := postgres.NewStore(pool)
	unitOfWork := store.UnitOfWork()
	sysClock := clock.New()
	rooms := livekit.NewRoomService(cfg.LiveKitAPIURL.String(), cfg.LiveKitAPIKey, cfg.LiveKitAPISecret)

	// Proceso periódico: reconciliación con el servidor de medios y
	// ejecución de los trabajos pendientes del outbox.
	reconcile := usecases.NewReconcileCalls(unitOfWork, sysClock, rooms, uuid.NewString)
	processOutbox := usecases.NewProcessOutbox(unitOfWork, sysClock, rooms, cfg.OutboxMaxAttempts, uuid.NewString)

	worker := jobs.NewWorker(cfg.WorkerInterval, []jobs.Task{
		{
			Name: "reconcile-calls",
			Run: func(ctx context.Context) error {
				result, err := reconcile.Execute(ctx)
				if err != nil {
					return err
				}
				if result.Cancelled > 0 || result.Ending > 0 || result.Ended > 0 || result.Orphans > 0 {
					logger.Info("reconciliación de llamadas",
						"canceladas", result.Cancelled,
						"en_cierre", result.Ending,
						"terminadas", result.Ended,
						"sin_sala", result.Orphans)
				}
				return nil
			},
		},
		{
			Name: "process-outbox",
			Run: func(ctx context.Context) error {
				result, err := processOutbox.Execute(ctx)
				if err != nil {
					return err
				}
				if result.Completed > 0 || result.Failed > 0 || result.Abandoned > 0 {
					logger.Info("trabajos pendientes",
						"completados", result.Completed,
						"fallidos", result.Failed,
						"abandonados", result.Abandoned)
				}
				return nil
			},
		},
	}, logger)
	go worker.Run(ctx)

	mux := http.NewServeMux()
	httpadapter.RegisterHealth(mux)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("API escuchando",
			"addr", cfg.HTTPAddr,
			"app_base_url", cfg.AppBaseURL.String(),
			"livekit_url", cfg.LiveKitURL.String(),
			"auth_mode", cfg.AuthMode,
		)
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}

	logger.Info("apagado ordenado iniciado")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("apagado del servidor HTTP: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	logger.Info("apagado completado")
	return nil
}

// runHealthcheck permite que la imagen mínima, sin shell ni curl, declare una
// sonda de salud invocando el propio binario.
func runHealthcheck(addr string) int {
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "HTTP_ADDR inválida:", err)
		return 1
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/health/live")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, "estado inesperado:", resp.Status)
		return 1
	}
	return 0
}
