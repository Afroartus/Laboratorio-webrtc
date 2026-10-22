package httpadapter

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

const readinessTimeout = 2 * time.Second

// ReadinessCheck comprueba una dependencia. El nombre se publica en
// /health/ready; el error solo se registra en el log.
type ReadinessCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

type healthResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}

// RegisterHealth registra las sondas de vida y de preparación. Solo devuelven
// información técnica mínima: estado global y "up"/"down" por dependencia.
func RegisterHealth(app *fiber.App, logger *slog.Logger, checks ...ReadinessCheck) {
	app.Get("/health/live", func(c fiber.Ctx) error {
		return c.JSON(healthResponse{Status: "ok"})
	})
	app.Get("/health/ready", func(c fiber.Ctx) error {
		ctx, cancel := context.WithTimeout(c.Context(), readinessTimeout)
		defer cancel()

		results := make([]error, len(checks))
		var wg sync.WaitGroup
		for i, chk := range checks {
			wg.Go(func() { results[i] = chk.Check(ctx) })
		}
		wg.Wait()

		resp := healthResponse{Status: "ready", Checks: make(map[string]string, len(checks))}
		for i, chk := range checks {
			if results[i] != nil {
				logger.Warn("dependencia no preparada", "check", chk.Name, "error", results[i])
				resp.Status = "not_ready"
				resp.Checks[chk.Name] = "down"
				continue
			}
			resp.Checks[chk.Name] = "up"
		}
		if resp.Status != "ready" {
			return c.Status(fiber.StatusServiceUnavailable).JSON(resp)
		}
		return c.JSON(resp)
	})
}
