package blob

import (
	"io"
	"net"
	"runtime"
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
// unwrapped for this check) and the platform has a kernel copy path into dst, it
// defers to io.Copy so that path (copy_file_range, splice, sendfile) applies.
// Otherwise it hides the descriptor-backed sides' ReadFrom/WriteTo, whose generic
// fallback would allocate a fresh 32 KiB buffer and ignore the pooled one.
func CopyReader(dst io.Writer, src io.Reader) (int64, error) {
	srcFD, dstFD := isFDBacked(src), isFDBacked(dst)
	if srcFD && dstFD && hasKernelCopyInto(dst) {
		return io.Copy(dst, src)
	}
	if srcFD {
		src = struct{ io.Reader }{src}
	}
	if dstFD {
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

// hasKernelCopyInto reports whether ReadFrom on the descriptor-backed dst can use a
// kernel copy path on this platform. It mirrors the build constraints of the standard
// library: net connections use sendfile (net/sendfile.go), while files only have a
// ReadFrom fast path on linux, freebsd and solaris (os/zero_copy_*.go). On darwin and
// windows, *os.File.ReadFrom is a plain read/write loop with its own buffer.
func hasKernelCopyInto(dst io.Writer) bool {
	if _, ok := dst.(net.Conn); ok {
		switch runtime.GOOS {
		case "linux", "darwin", "dragonfly", "freebsd", "solaris", "windows":
			return true
		}
		return false
	}
	switch runtime.GOOS {
	case "linux", "freebsd", "solaris":
		return true
	}
	return false
}
