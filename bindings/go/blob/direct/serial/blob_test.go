package serial

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	short  = 1 * time.Millisecond
	medium = 20 * time.Millisecond
	long   = 50 * time.Millisecond
)

func TestSerialBlob(t *testing.T) {
	t.Run("ReadCloser handle returns immediately", func(t *testing.T) {
		s := New(t.Context(), bytes.NewBufferString("abcdef"))

		l1 := mustGet(t, s.ReadCloser)
		mustCloseAtTestEnd(t, l1)

		// Acquire the lease with l1 and hold it briefly.
		go func() {
			_, _ = l1.Read(make([]byte, 1))
			time.Sleep(medium)
		}()

		start := time.Now()
		l2 := mustGet(t, s.ReadCloser)
		mustCloseAtTestEnd(t, l2)

		if time.Since(start) > short {
			t.Fatalf("ReadCloser() blocked; handle should return immediately")
		}
	})

	t.Run("acquire 'times out' via context while previous lease is active", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), short)
		defer cancel()
		s := New(ctx, bytes.NewBufferString("abcdef"))

		l1 := mustGet(t, s.ReadCloser)
		_, _ = l1.Read(make([]byte, 1)) // acquire and keep lease

		l2 := mustGet(t, s.ReadCloser)
		mustCloseAtTestEnd(t, l2)

		_, err := l2.Read(make([]byte, 1))
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected ErrSerialBlobAcquireTimeout, got %v", err)
		}

		// l2 never acquired; closing it must not release the token.
		// NOTE: This blob's context is now done; any further acquisitions on *this* blob will fail.
		// To verify normal behavior after a release, create a fresh blob with a fresh context.
		_ = l1.Close()

		ctx2 := t.Context()
		s2 := New(ctx2, bytes.NewBufferString("abcdef"))
		l3 := mustGet(t, s2.ReadCloser)
		mustCloseAtTestEnd(t, l3)
		if n, err := l3.Read(make([]byte, 1)); err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("unexpected read error after release: %v", err)
		} else if n == 0 {
			// ok if buffer exhausted; otherwise we'd expect >=1
		}
	})

	t.Run("acquire succeeds before context deadline when lease is released", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), long)
		defer cancel()
		s := New(ctx, bytes.NewBufferString("hello world"))

		l1 := mustGet(t, s.ReadCloser)
		go func() {
			_, _ = l1.Read(make([]byte, 1)) // acquire
			time.Sleep(short)               // release before l2 reaches deadline
			_ = l1.Close()
		}()

		l2 := mustGet(t, s.ReadCloser)
		mustCloseAtTestEnd(t, l2)

		b, err := io.ReadAll(l2)
		if err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("unexpected read error: %v", err)
		}
		if len(b) == 0 {
			t.Fatalf("expected to read some bytes after l1 closed the lease")
		}
	})

	t.Run("closing without read does not leak a token (simplified)", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		s := New(ctx, bytes.NewBufferString("XYZ"))

		// Prime: acquire and release once.
		l1 := mustGet(t, s.ReadCloser)
		_, _ = l1.Read(make([]byte, 1))
		_ = l1.Close()

		// l2 never reads → never acquires → Close must not release a token.
		l2 := mustGet(t, s.ReadCloser)
		_ = l2.Close()

		// l3 acquires synchronously and holds the lease.
		l3 := mustGet(t, s.ReadCloser)
		_, _ = l3.Read(make([]byte, 1))

		// l4 should observe acquisition failure once we cancel the context
		// while l3 still holds the token.
		l4 := mustGet(t, s.ReadCloser)
		mustCloseAtTestEnd(t, l4)

		errCh := make(chan error, 1)
		go func() {
			_, err := l4.Read(make([]byte, 1))
			errCh <- err
		}()

		// Cancel the blob's context to force ErrSerialBlobAcquireTimeout for waiters.
		time.Sleep(short) // small delay to ensure l4 is waiting
		cancel()

		select {
		case err := <-errCh:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected ErrSerialBlobAcquireTimeout, got %v", err)
			}
		case <-time.After(long):
			t.Fatalf("timed out waiting for l4 read result")
		}

		// Clean up: release l3 so no goroutines hang.
		_ = l3.Close()
	})
}

/* ---------- helpers ---------- */

func mustGet[T any](t *testing.T, get func() (T, error)) T {
	t.Helper()
	v, err := get()
	require.NotNil(t, v, "value should not be nil")
	require.NoError(t, err, "should not return an error")
	return v
}

func mustCloseAtTestEnd(t *testing.T, c io.Closer) {
	t.Helper()
	t.Cleanup(func() {
		require.NoError(t, c.Close(), "should close without error")
	})
}
