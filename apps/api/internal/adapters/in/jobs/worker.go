package jobs

import (
	"context"
	"log/slog"
	"time"
)

// Task es una operación periódica que ejecuta el worker.
type Task struct {
	// Name identifica la tarea en los registros.
	Name string
	// Run ejecuta la tarea. Un error no detiene el worker: se registra y la
	// tarea se reintenta en el siguiente ciclo.
	Run func(context.Context) error
}

// Worker ejecuta tareas periódicas hasta que se cancela el contexto.
type Worker struct {
	interval time.Duration
	tasks    []Task
	logger   *slog.Logger
}

// NewWorker devuelve el worker con su intervalo y sus tareas.
func NewWorker(interval time.Duration, tasks []Task, logger *slog.Logger) *Worker {
	return &Worker{interval: interval, tasks: tasks, logger: logger}
}

// Run ejecuta las tareas y espera al siguiente ciclo. La primera pasada es
// inmediata: al arrancar el servicio el estado ya queda conciliado.
func (w *Worker) Run(ctx context.Context) {
	w.runTasks(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("worker periódico detenido", "motivo", ctx.Err())
			return
		case <-ticker.C:
			w.runTasks(ctx)
		}
	}
}

// runTasks ejecuta cada tarea una vez.
func (w *Worker) runTasks(ctx context.Context) {
	for _, task := range w.tasks {
		if err := task.Run(ctx); err != nil {
			w.logger.Error("tarea periódica fallida", "tarea", task.Name, "error", err)
		}
	}
}
