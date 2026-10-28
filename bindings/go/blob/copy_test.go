package blob_test

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

// readSizeRecorder records the largest buffer passed to Read.
type readSizeRecorder struct {
	io.Reader
	maxRead int
}

func (r *readSizeRecorder) Read(p []byte) (int, error) {
	r.maxRead = max(r.maxRead, len(p))
	return r.Reader.Read(p)
}

func TestCopyReader(t *testing.T) {
	data := strings.Repeat("0123456789abcdef", 64*1024) // 1 MiB

	// *os.File.ReadFrom's generic fallback would read through its own 32 KiB buffer
	// instead of the pooled copy buffer, also when promoted through embedding.
	for name, wrap := range map[string]func(*os.File) io.Writer{
		"*os.File":          func(f *os.File) io.Writer { return f },
		"type embedding it": func(f *os.File) io.Writer { return struct{ *os.File }{f} },
	} {
		t.Run("into "+name+" from non-file reader uses the copy buffer", func(t *testing.T) {
			r := require.New(t)
			f, err := os.CreateTemp(t.TempDir(), "")
			r.NoError(err)
			t.Cleanup(func() { r.NoError(f.Close()) })

			src := &readSizeRecorder{Reader: strings.NewReader(data)}
			n, err := blob.CopyReader(wrap(f), src)
			r.NoError(err)
			r.Equal(int64(len(data)), n)
			r.Equal(blob.DefaultArchiveBlobBufferSize, src.maxRead)
			got, err := os.ReadFile(f.Name())
			r.NoError(err)
			r.Equal(data, string(got))
		})
	}

	t.Run("file to file", func(t *testing.T) {
		r := require.New(t)
		dir := t.TempDir()
		r.NoError(os.WriteFile(dir+"/src", []byte(data), 0o600))
		src, err := os.Open(dir + "/src")
		r.NoError(err)
		t.Cleanup(func() { r.NoError(src.Close()) })
		dst, err := os.Create(dir + "/dst")
		r.NoError(err)
		t.Cleanup(func() { r.NoError(dst.Close()) })

		n, err := blob.CopyReader(dst, src)
		r.NoError(err)
		r.Equal(int64(len(data)), n)
		got, err := os.ReadFile(dst.Name())
		r.NoError(err)
		r.Equal(data, string(got))

		t.Run("limited", func(t *testing.T) {
			r := require.New(t)
			_, err := src.Seek(0, io.SeekStart)
			r.NoError(err)
			r.NoError(dst.Truncate(0))
			_, err = dst.Seek(0, io.SeekStart)
			r.NoError(err)

			n, err := blob.CopyReaderN(dst, src, 100)
			r.NoError(err)
			r.Equal(int64(100), n)
			got, err := os.ReadFile(dst.Name())
			r.NoError(err)
			r.Equal(data[:100], string(got))
		})
	})
}

func TestCopyReaderN(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		n        int64
		wantN    int64
		wantErr  error
		wantOut  string
		wantRest string
	}{
		{name: "short source", src: "abc", n: 5, wantN: 3, wantErr: io.EOF, wantOut: "abc"},
		{name: "exact n leaves remainder unread", src: "abcdef", n: 4, wantN: 4, wantOut: "abcd", wantRest: "ef"},
		{name: "zero", src: "abc", n: 0, wantN: 0, wantRest: "abc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			src := strings.NewReader(tt.src)
			var buf bytes.Buffer
			n, err := blob.CopyReaderN(&buf, src, tt.n)
			r.Equal(tt.wantN, n)
			if tt.wantErr != nil {
				r.ErrorIs(err, tt.wantErr)
			} else {
				r.NoError(err)
			}
			r.Equal(tt.wantOut, buf.String())
			rest, err := io.ReadAll(src)
			r.NoError(err)
			r.Equal(tt.wantRest, string(rest))
		})
	}
}

func BenchmarkCopyReaderIntoFile(b *testing.B) {
	data := strings.Repeat("0123456789abcdef", 64*1024) // 1 MiB
	f, err := os.CreateTemp(b.TempDir(), "")
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = f.Close() })
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			b.Fatal(err)
		}
		if _, err := blob.CopyReader(f, struct{ io.Reader }{strings.NewReader(data)}); err != nil {
			b.Fatal(err)
		}
	}
}
