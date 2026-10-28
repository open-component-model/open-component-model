// Package gzippool reuses default-level gzip.Writers: a fresh writer allocates ~1 MB of
// compressor state, while Reset reuses it.
package gzippool

import (
	"compress/gzip"
	"io"
	"sync"
)

var pool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

// Get returns a pooled default-level gzip.Writer reset to write to w.
func Get(w io.Writer) *gzip.Writer {
	gw := pool.Get().(*gzip.Writer)
	gw.Reset(w)
	return gw
}

// Put returns gw to the pool. Call only after gw.Close(); gw must not be used afterwards.
func Put(gw *gzip.Writer) {
	// Drop the reference to the caller's writer so the pool does not keep it alive.
	gw.Reset(io.Discard)
	pool.Put(gw)
}
