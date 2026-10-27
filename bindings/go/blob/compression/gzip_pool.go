package compression

import (
	"compress/gzip"
	"io"
	"sync"
)

// gzipWriterPool reuses gzip.Writers: a fresh writer allocates ~1 MB of compressor state,
// while Reset reuses it.
var gzipWriterPool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

// GetGzipWriter returns a pooled default-level gzip.Writer reset to write to w.
func GetGzipWriter(w io.Writer) *gzip.Writer {
	gw := gzipWriterPool.Get().(*gzip.Writer)
	gw.Reset(w)
	return gw
}

// PutGzipWriter returns gw to the pool. Call only after gw.Close(); gw must not be used afterwards.
func PutGzipWriter(gw *gzip.Writer) {
	// Drop the reference to the caller's writer so the pool does not keep it alive.
	gw.Reset(io.Discard)
	gzipWriterPool.Put(gw)
}
