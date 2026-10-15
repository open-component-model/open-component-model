package client

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/opencontainers/go-digest"
)

// uploadBody hashes source bytes and retains read and verification errors that an early
// HTTP response can hide. Close must precede Digest and Complete.
// The source Close must unblock a concurrent Read, as required for HTTP bodies.
type uploadBody struct {
	source    io.ReadCloser
	size      int64
	digester  digest.Digester
	mu        sync.Mutex
	read      int64
	eof       bool
	readErr   error
	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

func NewUploadBody(source io.ReadCloser, size int64, algorithm digest.Algorithm) *uploadBody {
	return &uploadBody{source: source, size: size, digester: algorithm.Digester()}
}

func (b *uploadBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	n, err := b.source.Read(p)
	if n > 0 {
		_, _ = b.digester.Hash().Write(p[:n])
		b.read += int64(n)
	}
	if errors.Is(err, io.EOF) {
		b.eof = true
	} else if err != nil {
		b.readErr = err
	}
	return n, err
}

func (b *uploadBody) Close() error {
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		// Close first to unblock a source read before waiting for its hash update.
		err := b.source.Close()
		b.mu.Lock()
		defer b.mu.Unlock()
		b.closeErr = errors.Join(err, b.readErr)
		if !b.complete() {
			b.closeErr = errors.Join(b.closeErr, fmt.Errorf("incomplete upload: source was not read in full"))
		}
	})
	return b.closeErr
}

func (b *uploadBody) complete() bool {
	if b.size >= 0 {
		return b.read == b.size
	}
	return b.eof
}

func (b *uploadBody) Complete() bool {
	return b.complete() && b.closeErr == nil
}

func (b *uploadBody) Digest() digest.Digest {
	return b.digester.Digest()
}
