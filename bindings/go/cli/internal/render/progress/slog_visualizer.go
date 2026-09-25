package progress

import (
	"log/slog"
	"time"
)

// SlogVisualizer is a slog-based visualizer for non-terminal environments.
type SlogVisualizer[T any] struct {
	name  string
	start time.Time
}

func (v *SlogVisualizer[T]) Begin(name string) {
	v.name = name
	v.start = time.Now()
	slog.Info(v.name + ": operation starting")
}

func (v *SlogVisualizer[T]) HandleEvent(event Event[T]) {
	var attrs []any
	attrs = append(attrs, "item", event.Name)
	if event.Duration > 0 {
		attrs = append(attrs, "duration", event.Duration.Round(time.Second).String())
	}
	switch event.State {
	case Running:
		slog.Info(v.name+": item in-progress", attrs...)
	case Completed:
		slog.Info(v.name+": item completed", attrs...)
	case Failed:
		slog.Error(v.name+": item failed", append(attrs, "error", event.Err)...)
	case Cancelled:
		slog.Warn(v.name+": item cancelled", attrs...)
	}
}

func (v *SlogVisualizer[T]) End(err error) {
	var attrs []any
	if !v.start.IsZero() {
		attrs = append(attrs, "duration", time.Since(v.start).Round(time.Second).String())
	}
	if err != nil {
		slog.Error(v.name+": operation failed", append(attrs, "error", err)...)
	} else {
		slog.Info(v.name+": operation finished", attrs...)
	}
}
