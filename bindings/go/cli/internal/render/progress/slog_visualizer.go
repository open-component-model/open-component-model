package progress

import (
	"log/slog"
	"time"
)

// SlogVisualizer is a slog-based visualizer for non-terminal environments.
type SlogVisualizer[T any] struct {
	name        string
	start       time.Time
	concurrency int
}

// SetConcurrency implements [ConcurrencyAware]. It records how many items may
// be processed in parallel so the count is logged with the operation.
func (v *SlogVisualizer[T]) SetConcurrency(runners int) {
	v.concurrency = runners
}

func (v *SlogVisualizer[T]) Begin(name string) {
	v.name = name
	v.start = time.Now()
	if v.concurrency > 0 {
		slog.Info(v.name+": operation starting", "runners", v.concurrency)
		return
	}
	slog.Info(v.name + ": operation starting")
}

func (v *SlogVisualizer[T]) HandleEvent(event Event[T]) {
	var attrs []any
	attrs = append(attrs, "item", event.Name)
	if event.InFlight > 0 {
		attrs = append(attrs, "inFlight", event.InFlight)
		if v.concurrency > 0 {
			attrs = append(attrs, "runners", v.concurrency)
		}
	}
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
