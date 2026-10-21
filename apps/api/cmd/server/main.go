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

	httpadapter "webrtc/api/internal/adapters/in/http"
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
