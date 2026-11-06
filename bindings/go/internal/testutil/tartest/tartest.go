// Package tartest builds tar archives in tests.
//
// [Gzipped] returns a gzip-compressed tar archive with one regular file per [Entry], in the
// given order:
//
//	archive := tartest.Gzipped(t,
//		tartest.Entry{Name: "repo/README.md", Data: []byte("# hi")},
//		tartest.Entry{Name: "repo/main.go", Data: src},
//	)
package tartest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/stretchr/testify/require"
)

// Entry is a regular file in an archive.
type Entry struct {
	Name string
	Data []byte
}

// Gzipped returns a gzip-compressed tar archive holding the entries in the given order, each
// a regular file with mode 0644.
func Gzipped(t *testing.T, entries ...Entry) []byte {
	t.Helper()
	r := require.New(t)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		r.NoError(tw.WriteHeader(&tar.Header{Name: e.Name, Mode: 0o644, Size: int64(len(e.Data)), Typeflag: tar.TypeReg}))
		_, err := tw.Write(e.Data)
		r.NoError(err)
	}
	r.NoError(tw.Close())
	r.NoError(gz.Close())

	return buf.Bytes()
}
