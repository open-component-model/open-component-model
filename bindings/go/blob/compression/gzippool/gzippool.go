// Package gzippool reuses gzip.Writers across compression streams. A fresh
// gzip.Writer allocates about 1 MB of compressor state; a pooled one is reset instead.
//
// Contract:
//   - Get returns a writer at gzip.DefaultCompression that writes to w. Its state,
//     including Header, is identical to gzip.NewWriter(w): the compressed output is
//     byte-for-byte the same as from a fresh writer, so digests over it are stable
//     regardless of what the writer was used for before.
//   - The caller owns the writer exclusively until it calls Put. Callers that change
//     Header or call Reset themselves still get a clean writer from the next Get.
//   - Close the writer before Put to flush the stream. Put discards anything still
//     buffered, so on error paths a writer may be returned without Close.
//   - After Put, the caller must not use the writer again. The pool does not
//     retain the destination writer passed to Get.
//   - Get and Put are safe for concurrent use.
package gzippool

import (
	"compress/gzip"
	"io"
	"sync"
)

var pool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

// Get returns a writer at gzip.DefaultCompression that writes to w, in the same state
// as gzip.NewWriter(w). Return it with Put.
func Get(w io.Writer) *gzip.Writer {
	gw := pool.Get().(*gzip.Writer)
	gw.Reset(w)
	return gw
}

// Put returns gw to the pool. Close gw first to flush it; gw must not be used
// afterwards.
func Put(gw *gzip.Writer) {
	// Reset also clears Header and drops the reference to the caller's writer.
	gw.Reset(io.Discard)
	pool.Put(gw)
}
