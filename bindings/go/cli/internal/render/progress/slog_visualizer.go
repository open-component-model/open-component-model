package progress

import "log/slog"

// SlogVisualizer is a slog-based visualizer for non-terminal environments.
type SlogVisualizer[T any] struct {
	name string
}

func (v *SlogVisualizer[T]) Begin(name string) {
	v.name = name
	slog.Info(v.name + ": operation starting")
}

func (v *SlogVisualizer[T]) HandleEvent(event Event[T]) {
	switch event.State {
	case Running:
		slog.Info(v.name+": item in-progress", "item", event.Name)
	case Completed:
		slog.Info(v.name+": item completed", "item", event.Name)
	case Failed:
		slog.Error(v.name+": item failed", "item", event.Name, "error", event.Err)
	case Cancelled:
		slog.Warn(v.name+": item cancelled", "item", event.Name)
	}
}

func (v *SlogVisualizer[T]) End(err error) {
	if err != nil {
		slog.Error(v.name+": operation failed", "error", err)
	} else {
		slog.Info(v.name + ": operation finished")
	}
}
