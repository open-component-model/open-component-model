package progress

import (
	"bytes"
	"fmt"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureSlog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	original := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(original)
	fn()
	return buf.String()
}

// --- SlogVisualizer tests ---

func TestSlogVisualizer_Begin(t *testing.T) {
	output := captureSlog(t, func() {
		v := &SlogVisualizer[any]{}
		v.Begin("Resolving")
	})
	assert.Contains(t, output, "Resolving: operation starting")
	assert.Contains(t, output, "level=INFO")
}

func TestSlogVisualizer_End_Success(t *testing.T) {
	output := captureSlog(t, func() {
		v := &SlogVisualizer[any]{}
		v.Begin("Resolving")
		v.End(nil)
	})
	assert.Contains(t, output, "Resolving: operation finished")
}

func TestSlogVisualizer_End_Error(t *testing.T) {
	output := captureSlog(t, func() {
		v := &SlogVisualizer[any]{}
		v.Begin("Resolving")
		v.End(fmt.Errorf("connection refused"))
	})
	assert.Contains(t, output, "Resolving: operation failed")
	assert.Contains(t, output, "connection refused")
	assert.Contains(t, output, "level=ERROR")
}

func TestSlogVisualizer_HandleEvent(t *testing.T) {
	tests := []struct {
		name     string
		state    State
		err      error
		contains []string
	}{
		{"running", Running, nil, []string{"item in-progress", "level=INFO"}},
		{"completed", Completed, nil, []string{"item completed", "level=INFO"}},
		{"failed", Failed, fmt.Errorf("timeout"), []string{"item failed", "level=ERROR", "timeout"}},
		{"cancelled", Cancelled, nil, []string{"item cancelled", "level=WARN"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureSlog(t, func() {
				v := &SlogVisualizer[any]{}
				v.Begin("Transfer")
				v.HandleEvent(Event[any]{ID: "1", Name: "component-a", State: tt.state, Err: tt.err})
			})
			for _, s := range tt.contains {
				assert.Contains(t, output, s)
			}
			assert.Contains(t, output, "item=component-a")
		})
	}
}

// --- SyncBuffer tests ---

func TestSyncBuffer_WriteAndDrain(t *testing.T) {
	buf := &SyncBuffer{}

	n, err := buf.Write([]byte("hello "))
	require.NoError(t, err)
	assert.Equal(t, 6, n)

	n, err = buf.Write([]byte("world"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)

	assert.Equal(t, 11, buf.Len())

	content := buf.DrainString()
	assert.Equal(t, "hello world", content)
	assert.Equal(t, 0, buf.Len(), "buffer should be empty after drain")
}

func TestSyncBuffer_DrainString_EmptyBuffer(t *testing.T) {
	buf := &SyncBuffer{}
	content := buf.DrainString()
	assert.Empty(t, content)
}

func TestSyncBuffer_DrainString_ResetsBuffer(t *testing.T) {
	buf := &SyncBuffer{}
	buf.Write([]byte("first"))
	buf.DrainString()

	buf.Write([]byte("second"))
	content := buf.DrainString()
	assert.Equal(t, "second", content, "drain should reset; second write should not contain first")
}

func TestSyncBuffer_Len_ReflectsWrites(t *testing.T) {
	buf := &SyncBuffer{}
	assert.Equal(t, 0, buf.Len())

	buf.Write([]byte("abc"))
	assert.Equal(t, 3, buf.Len())
}

// --- Handler tests ---

func TestResolveHandlerLevel(t *testing.T) {
	tests := []struct {
		name     string
		handler  slog.Handler
		expected slog.Level
	}{
		{"nil handler defaults to Info", nil, slog.LevelInfo},
		{"debug handler returns Debug", slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug}), slog.LevelDebug},
		{"info handler returns Info", slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelInfo}), slog.LevelInfo},
		{"warn handler returns Warn", slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn}), slog.LevelWarn},
		{"error handler returns Error", slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelError}), slog.LevelError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			level := resolveHandlerLevel(tt.handler)
			assert.Equal(t, tt.expected, level)
		})
	}
}

func TestNewBufferedHandler_PreservesLevel(t *testing.T) {
	t.Run("writes to buffer", func(t *testing.T) {
		buf := &SyncBuffer{}
		h := newBufferedHandler(buf, nil)

		logger := slog.New(h)
		logger.Info("test message")

		content := buf.DrainString()
		require.NotEmpty(t, content)
		assert.Contains(t, content, "test message")
	})

	t.Run("nil previous handler defaults to info level", func(t *testing.T) {
		buf := &SyncBuffer{}
		h := newBufferedHandler(buf, nil)

		assert.False(t, h.Enabled(t.Context(), slog.LevelDebug))
		assert.True(t, h.Enabled(t.Context(), slog.LevelInfo))
	})
}
