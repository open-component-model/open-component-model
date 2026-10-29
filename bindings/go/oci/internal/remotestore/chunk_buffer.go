package remotestore

import (
	"bytes"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
)

// chunkBufferPools holds one pool per chunk size (int64 -> *sync.Pool of *chunkBuffer).
// Chunk sizes come from the configured ChunkSize or a registry-advertised
// OCI-Chunk-Min-Length, so only a handful of distinct keys exist in practice.
var chunkBufferPools sync.Map

// chunkBuffer is a reusable PATCH buffer of up to MaxChunkSize bytes.
//
// The transport may keep reading a request body after RoundTrip returns (see
// [net/http.RoundTripper]), including bodies recreated through GetBody for an
// auth or redirect retry. The buffer therefore counts the bodies handed out over
// it and is only overwritten or pooled once all of them are closed; a buffer
// with a body still open is abandoned to the garbage collector instead.
type chunkBuffer struct {
	data []byte
	open atomic.Int64
}

func getChunkBuffer(size int64) *chunkBuffer {
	pool, ok := chunkBufferPools.Load(size)
	if !ok {
		pool, _ = chunkBufferPools.LoadOrStore(size, &sync.Pool{New: func() any {
			return &chunkBuffer{data: make([]byte, size)}
		}})
	}
	return pool.(*sync.Pool).Get().(*chunkBuffer)
}

// inUse reports whether a request body over the buffer may still be read.
func (b *chunkBuffer) inUse() bool {
	return b.open.Load() != 0
}

// release returns the buffer to its pool unless a request body over it is still open.
func (b *chunkBuffer) release() {
	if b.inUse() {
		return
	}
	if pool, ok := chunkBufferPools.Load(int64(len(b.data))); ok {
		pool.(*sync.Pool).Put(b)
	}
}

// body returns a request body over the first n bytes that is tracked until closed.
func (b *chunkBuffer) body(n int) io.ReadCloser {
	b.open.Add(1)
	return &chunkBody{reader: bytes.NewReader(b.data[:n]), buf: b}
}

// chunkBody is a request body over a chunkBuffer. Read and Close are mutually
// exclusive because the transport may close the body from another goroutine
// while a read is still in flight (e.g. HTTP/2 on cancellation); the buffer must
// only become reusable once no read can touch it anymore.
type chunkBody struct {
	mu     sync.Mutex
	reader *bytes.Reader
	buf    *chunkBuffer
	closed bool
}

func (c *chunkBody) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, http.ErrBodyReadAfterClose
	}
	return c.reader.Read(p)
}

func (c *chunkBody) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		c.buf.open.Add(-1)
	}
	return nil
}
