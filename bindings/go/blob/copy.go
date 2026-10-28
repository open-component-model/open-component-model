package blob

import (
	"io"
	"sync"
	"syscall"
)

var copyBufPool = sync.Pool{New: func() any {
	b := make([]byte, DefaultArchiveBlobBufferSize)
	return &b
}}

// CopyReader copies src to dst like io.Copy, using a pooled buffer.
//
// When both sides are backed by a file descriptor (they implement syscall.Conn, as
// *os.File, types embedding it, and net connections do; a src *io.LimitedReader is
// unwrapped for this check), it defers to io.Copy so kernel zero-copy paths
// (copy_file_range, splice, sendfile) apply. Otherwise it hides the descriptor-backed
// side's ReadFrom/WriteTo, whose generic fallback would allocate a fresh 32 KiB
// buffer and ignore the pooled one.
func CopyReader(dst io.Writer, src io.Reader) (int64, error) {
	srcFD, dstFD := isFDBacked(src), isFDBacked(dst)
	switch {
	case srcFD && dstFD:
		return io.Copy(dst, src)
	case srcFD:
		src = struct{ io.Reader }{src}
	case dstFD:
		dst = struct{ io.Writer }{dst}
	}
	bufp := copyBufPool.Get().(*[]byte)
	defer copyBufPool.Put(bufp)
	return io.CopyBuffer(dst, src, *bufp)
}

// CopyReaderN copies n bytes from src to dst like io.CopyN, using a pooled buffer
// (see CopyReader). It returns io.EOF if fewer than n bytes were copied.
func CopyReaderN(dst io.Writer, src io.Reader, n int64) (int64, error) {
	written, err := CopyReader(dst, io.LimitReader(src, n))
	if written == n {
		return n, nil
	}
	if written < n && err == nil {
		err = io.EOF
	}
	return written, err
}

func isFDBacked(v any) bool {
	if lr, ok := v.(*io.LimitedReader); ok {
		v = lr.R
	}
	_, ok := v.(syscall.Conn)
	return ok
}
