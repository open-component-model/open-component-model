package simple

import (
	"io"
	"log/slog"

	"ocm.software/open-component-model/cli/internal/render/progress"
)

// simpleVisualizer logs progress events using a structured logger.
type simpleVisualizer[T any] struct {
	logger *slog.Logger
}

// NewSimpleVisualizer creates a factory function for SimpleVisualizer.
func NewSimpleVisualizer[T any](logger *slog.Logger) progress.VisualizerFactory[T] {
	return func(_ io.Writer, _ int) progress.Visualizer[T] {
		return &simpleVisualizer[T]{
			logger: logger,
		}
	}
}

// HandleEvent logs the progress event.
func (v *simpleVisualizer[T]) HandleEvent(event progress.Event[T]) {
	switch event.State {
	case progress.Running:
		v.logger.Debug("transformation started", "id", event.ID)
	case progress.Completed:
		v.logger.Debug("transformation completed", "id", event.ID)
	case progress.Failed:
		v.logger.Error("transformation failed", "id", event.ID, "error", event.Err)
	case progress.Cancelled:
		v.logger.Warn("transformation cancelled", "id", event.ID)
	}
}

// Summary implements the Visualizer interface.
func (v *simpleVisualizer[T]) Summary(err error) {
	if err != nil {
		v.logger.Error("execution failed", "error", err)
	}
}
