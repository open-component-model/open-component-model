package blob

import (
	"io"
	"os"
	"sync"
)

var copyBufPool = sync.Pool{New: func() any {
	b := make([]byte, DefaultArchiveBlobBufferSize)
	return &b
}}

// CopyReader copies src to dst like io.Copy, using a pooled buffer. When both sides are
// *os.File it defers to io.Copy so kernel zero-copy (copy_file_range/splice) applies;
// otherwise it hides *os.File's ReadFrom/WriteTo, whose generic fallback would allocate
// a fresh 32 KiB buffer and ignore ours.
func CopyReader(dst io.Writer, src io.Reader) (int64, error) {
	sf, srcIsFile := src.(*os.File)
	df, dstIsFile := dst.(*os.File)
	switch {
	case srcIsFile && dstIsFile:
		return io.Copy(df, sf)
	case srcIsFile:
		src = struct{ io.Reader }{sf}
	case dstIsFile:
		dst = struct{ io.Writer }{df}
	}
	bufp := copyBufPool.Get().(*[]byte)
	defer copyBufPool.Put(bufp)
	return io.CopyBuffer(dst, src, *bufp)
}

// CopyReaderN copies n bytes from src to dst like io.CopyN, using a pooled buffer
// (see CopyReader). It returns io.EOF if fewer than n bytes were copied.
func CopyReaderN(dst io.Writer, src io.Reader, n int64) (int64, error) {
	if df, ok := dst.(*os.File); ok {
		if sf, ok := src.(*os.File); ok {
			return io.CopyN(df, sf, n)
		}
	}
	written, err := CopyReader(dst, io.LimitReader(src, n))
	if written == n {
		return n, nil
	}
	if written < n && err == nil {
		err = io.EOF
	}
	return written, err
}
