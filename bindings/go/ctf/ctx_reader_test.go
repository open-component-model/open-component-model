package ctf

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCtxReader_Success(t *testing.T) {
	ctx := t.Context()
	data := []byte("hello world")
	reader := bytes.NewReader(data)

	ctxReader, err := newCtxReader(ctx, reader)
	require.NoError(t, err)
	buf := make([]byte, len(data))
	n, err := ctxReader.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf)
}

func TestNewCtxReader_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel the context immediately

	data := []byte("test")
	reader := bytes.NewReader(data)

	ctxReader, err := newCtxReader(ctx, reader)
	require.NoError(t, err)
	buf := make([]byte, len(data))

	n, err := ctxReader.Read(buf)
	assert.Equal(t, 0, n)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestNewCtxReader_WithDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()

	data := []byte("timeout test")
	reader := bytes.NewReader(data)

	ctxReader, err := newCtxReader(ctx, reader)
	require.NoError(t, err)
	buf := make([]byte, len(data))

	n, err := ctxReader.Read(buf)
	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf)

	time.Sleep(20 * time.Millisecond) // Wait for the context to timeout
	n, err = ctxReader.Read(buf)
	assert.Equal(t, 0, n)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}
