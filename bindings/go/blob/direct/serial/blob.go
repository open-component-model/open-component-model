package serial

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
)

// Blob is a blob that hands out [io.ReadCloser] instances ("leases") that serialize access to a single
// underlying [io.Reader]. You can call ReadCloser at any time, also concurrently; the *first* [io.Reader.Read]
// on that lease will block until the previous lease is closed (or until the
// context is canceled, whatever comes first).
//
// IMPORTANT: The underlying reader is shared and stateful. Each lease continues
// reading from wherever the previous lease stopped, if it does not support io.Seeker.
//
// If the stateful reader does support io.Seeker, acquiring a lease will reset the reader position to io.SeekStart.
// If you need independent positions and concurrent reads, your source must support io.ReaderAt, at which
// point it can be used without this serial reader.
type Blob struct {
	ctx    context.Context
	reader io.Reader
	token  chan struct{} // capacity 1: acts as a binary semaphore
}

// New creates a Blob within a context. If the context is canceled,
// any leases being acquired will see ctx.Err().
func New(ctx context.Context, reader io.Reader) *Blob {
	s := &Blob{
		ctx:    ctx,
		reader: reader,
		token:  make(chan struct{}, 1),
	}
	// Put initial token to indicate "available".
	s.token <- struct{}{}
	return s
}

// ReadCloser returns a leased reader immediately. The first Read() on that
// lease will try to acquire the token (blocking until available or until ctx is canceled).
func (s *Blob) ReadCloser() (io.ReadCloser, error) {
	l := &lease{
		parent: s,
		reader: s.reader,
	}

	// acquired is true if the token was acquired.
	var acquired atomic.Bool

	// Lazily acquire on the first Read, at most once, and cache the error.
	l.acquire = sync.OnceValue(func() error {
		select {
		case <-l.parent.token:
			acquired.Store(true)
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
		// If underlying reader is a seeker, reset to start for this lease.
		// Otherwise, the reader is stateful and we can't reset.
		// Then we do best effort and leave it up to the caller to handle errors.
		if seeker, ok := l.reader.(io.Seeker); ok {
			_, err := seeker.Seek(0, io.SeekStart)
			return err
		}
		return nil
	})
	l.close = func() error {
		if acquired.Load() {
			// Release the token if we did acquire it before.
			l.parent.token <- struct{}{}
		}
		return nil
	}

	return l, nil
}

type lease struct {
	// the parent Blob from which the reader was leased from
	parent *Blob
	// the underlying reader that is leased
	reader io.Reader

	// acquire tries to acquire the token required to read.
	acquire func() error
	// idempotent Close which releases the token if acquired.
	close func() error
}

func (l *lease) Read(p []byte) (int, error) {
	if err := l.acquire(); err != nil {
		return 0, err
	}
	return l.reader.Read(p)
}

// Close releases the token if (and only if) it was acquired. Idempotent.
// Not safe to call Read concurrently with Close.
func (l *lease) Close() error {
	return l.close()
}
