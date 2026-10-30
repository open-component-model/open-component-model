package verified_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/verified"
)

type closeCountingReader struct {
	io.Reader
	closes int
}

func (r *closeCountingReader) Close() error { r.closes++; return nil }

// opener is a source that opens a fresh reader over content on every read and records it.
type opener struct {
	content []byte
	opened  []*closeCountingReader
}

func (o *opener) ReadCloser() (io.ReadCloser, error) {
	src := &closeCountingReader{Reader: bytes.NewReader(o.content)}
	o.opened = append(o.opened, src)
	return src, nil
}

func TestBlob(t *testing.T) {
	data := []byte("verified blob content")
	meta := verified.Metadata{Digest: digest.FromBytes(data), Size: int64(len(data)), MediaType: "text/plain"}

	t.Run("metadata is reported without opening the content", func(t *testing.T) {
		r := require.New(t)
		src := &opener{content: data}
		b := verified.New(src, meta)
		dig, ok := b.Digest()
		r.True(ok)
		r.Equal(meta.Digest.String(), dig)
		r.Equal(meta.Size, b.Size())
		mediaType, ok := b.MediaType()
		r.True(ok)
		r.Equal(meta.MediaType, mediaType)
		r.Empty(src.opened)
	})

	t.Run("each read opens fresh content and closes it", func(t *testing.T) {
		r := require.New(t)
		src := &opener{content: data}
		b := verified.New(src, meta)
		for range 2 {
			rc, err := b.ReadCloser()
			r.NoError(err)
			got, err := io.ReadAll(rc)
			r.NoError(err)
			r.NoError(rc.Close())
			r.Equal(data, got)
		}
		r.Len(src.opened, 2)
		for _, opened := range src.opened {
			r.Equal(1, opened.closes)
		}
	})

	t.Run("reading exactly the known size verifies without EOF", func(t *testing.T) {
		r := require.New(t)
		rc, err := verified.New(&opener{content: data}, meta).ReadCloser()
		r.NoError(err)
		_, err = io.CopyN(io.Discard, rc, meta.Size)
		r.NoError(err)
		r.NoError(rc.Close())
	})

	t.Run("a partial read fails on close and still releases the content", func(t *testing.T) {
		r := require.New(t)
		src := &opener{content: data}
		rc, err := verified.New(src, meta).ReadCloser()
		r.NoError(err)
		_, err = rc.Read(make([]byte, 4))
		r.NoError(err)
		r.ErrorContains(rc.Close(), "size mismatch")
		r.Equal(1, src.opened[0].closes)
	})

	t.Run("unknown size and digest accept any content read in full", func(t *testing.T) {
		r := require.New(t)
		b := verified.New(&opener{content: data}, verified.Metadata{Size: blob.SizeUnknown})
		_, ok := b.Digest()
		r.False(ok)
		_, ok = b.MediaType()
		r.False(ok)
		rc, err := b.ReadCloser()
		r.NoError(err)
		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.NoError(rc.Close())
		r.Equal(data, got)
	})

	t.Run("a partial read of unknown size fails on close", func(t *testing.T) {
		r := require.New(t)
		rc, err := verified.New(&opener{content: data}, verified.Metadata{Size: blob.SizeUnknown}).ReadCloser()
		r.NoError(err)
		_, err = rc.Read(make([]byte, 4))
		r.NoError(err)
		r.ErrorContains(rc.Close(), "incomplete read")
	})

	t.Run("mismatching content fails on read and on close", func(t *testing.T) {
		for name, content := range map[string][]byte{
			"same size, other digest": []byte("tampered blob content"),
			"shorter":                 data[:len(data)-1],
			"longer":                  append(bytes.Clone(data), '!'),
		} {
			t.Run(name, func(t *testing.T) {
				r := require.New(t)
				rc, err := verified.New(&opener{content: content}, meta).ReadCloser()
				r.NoError(err)
				_, err = io.ReadAll(rc)
				r.ErrorContains(err, "mismatch")
				r.ErrorContains(rc.Close(), "mismatch")
			})
		}
	})

	t.Run("an invalid digest fails before opening the content", func(t *testing.T) {
		r := require.New(t)
		src := &opener{content: data}
		_, err := verified.New(src, verified.Metadata{Digest: "unknown:abc", Size: meta.Size}).ReadCloser()
		r.ErrorContains(err, "invalid expected digest")
		r.Empty(src.opened)
	})
}

func TestExpected(t *testing.T) {
	data := []byte("verified blob content")
	meta := verified.Metadata{Digest: digest.FromBytes(data), Size: int64(len(data))}

	t.Run("a reader of a blob reports what it verifies, also behind NopCloser", func(t *testing.T) {
		r := require.New(t)
		src := &opener{content: data}
		rc, err := verified.New(src, meta).ReadCloser()
		r.NoError(err)
		for _, reader := range []io.Reader{rc, verified.NopCloser(rc)} {
			dig, size, ok := verified.Expected(reader)
			r.True(ok)
			r.Equal(meta.Digest, dig)
			r.Equal(meta.Size, size)
		}
		r.NoError(verified.NopCloser(rc).Close())
		r.Zero(src.opened[0].closes, "NopCloser must not close the source")
	})

	t.Run("other readers are not trusted", func(t *testing.T) {
		r := require.New(t)
		for _, reader := range []io.Reader{bytes.NewReader(data), verified.NopCloser(bytes.NewReader(data))} {
			_, _, ok := verified.Expected(reader)
			r.False(ok)
		}
	})
}
