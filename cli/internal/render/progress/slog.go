package progress

import (
	"bytes"
	"context"
	"log/slog"
)

// LogBufferAware is an optional interface for visualizers that can display buffered log output.
// If a Visualizer implements this, the Tracker will pass the slog buffer so the visualizer
// can render log lines inline (e.g., above the progress bar).
type LogBufferAware interface {
	SetLogBuffer(buf *bytes.Buffer)
}

// bufferedHandler wraps a slog.Handler to redirect output to a buffer.
type bufferedHandler struct {
	inner slog.Handler
}

func newBufferedHandler(buf *bytes.Buffer) *bufferedHandler {
	return &bufferedHandler{inner: slog.NewTextHandler(buf, nil)}
}

func (b *bufferedHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return b.inner.Enabled(ctx, level)
}

func (b *bufferedHandler) Handle(ctx context.Context, record slog.Record) error {
	return b.inner.Handle(ctx, record)
}

func (b *bufferedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &bufferedHandler{inner: b.inner.WithAttrs(attrs)}
}

func (b *bufferedHandler) WithGroup(name string) slog.Handler {
	return &bufferedHandler{inner: b.inner.WithGroup(name)}
}
