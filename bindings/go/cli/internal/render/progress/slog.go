package progress

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
)

// SyncBuffer is a thread-safe bytes.Buffer.
// Writes from the slog handler and reads/drains from visualizers happen concurrently.
type SyncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// Write implements io.Writer (called by slog.TextHandler).
func (sb *SyncBuffer) Write(p []byte) (int, error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

// DrainString returns the buffered content and resets the buffer atomically.
func (sb *SyncBuffer) DrainString() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	s := sb.buf.String()
	sb.buf.Reset()
	return s
}

// Len returns the number of unread bytes.
func (sb *SyncBuffer) Len() int {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Len()
}

// LogBufferAware is an optional interface for visualizers that can display buffered log output.
// If a Visualizer implements this, the Tracker will pass the slog buffer so the visualizer
// can render log lines inline (e.g., above the progress bar).
type LogBufferAware interface {
	SetLogBuffer(buf *SyncBuffer)
}

// bufferedHandler wraps a slog.Handler to redirect output to a buffer.
// It preserves the log level from the previous handler so that level
// filtering (e.g. --loglevel error) is respected during progress tracking.
type bufferedHandler struct {
	inner slog.Handler
}

func newBufferedHandler(buf *SyncBuffer, previousHandler slog.Handler) *bufferedHandler {
	level := resolveHandlerLevel(previousHandler)
	return &bufferedHandler{
		inner: slog.NewTextHandler(buf, &slog.HandlerOptions{Level: level}),
	}
}

// resolveHandlerLevel determines the effective minimum log level of a handler
// by probing which levels are enabled.
func resolveHandlerLevel(h slog.Handler) slog.Level {
	if h == nil {
		return slog.LevelInfo
	}
	for _, l := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
		if h.Enabled(context.Background(), l) {
			return l
		}
	}
	return slog.LevelError
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
