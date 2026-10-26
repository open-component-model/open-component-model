package blob_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
)

type closeCountingReader struct {
	io.Reader
	closes int
}

func (r *closeCountingReader) Close() error { r.closes++; return nil }

func TestDescriptorBlob(t *testing.T) {
	data := []byte("descriptor blob content")
	desc := ocispec.Descriptor{Digest: digest.FromBytes(data), Size: int64(len(data))}

	// newBlob returns a blob over data and the readers it opened so far.
	newBlob := func(content []byte) (*ociblob.DescriptorBlob, *[]*closeCountingReader) {
		var opened []*closeCountingReader
		return ociblob.NewDescriptorBlob(func() (io.ReadCloser, error) {
			src := &closeCountingReader{Reader: bytes.NewReader(content)}
			opened = append(opened, src)
			return src, nil
		}, desc), &opened
	}

	t.Run("content is not opened before it is read", func(t *testing.T) {
		r := require.New(t)
		b, opened := newBlob(data)
		dig, ok := b.Digest()
		r.True(ok)
		r.Equal(desc.Digest.String(), dig)
		r.Equal(desc.Size, b.Size())
		r.Empty(*opened)
	})

	t.Run("each read opens fresh content", func(t *testing.T) {
		r := require.New(t)
		b, opened := newBlob(data)
		for range 2 {
			rc, err := b.ReadCloser()
			r.NoError(err)
			got, err := io.ReadAll(rc)
			r.NoError(err)
			r.NoError(rc.Close())
			r.Equal(data, got)
		}
		r.Len(*opened, 2)
		for _, src := range *opened {
			r.Equal(1, src.closes)
		}
	})

	t.Run("a partially read reader releases the content although verification fails", func(t *testing.T) {
		r := require.New(t)
		b, opened := newBlob(data)
		rc, err := b.ReadCloser()
		r.NoError(err)
		_, err = rc.Read(make([]byte, 4))
		r.NoError(err)
		r.Error(rc.Close())
		r.Equal(1, (*opened)[0].closes)
	})

	t.Run("content not matching the descriptor fails on close", func(t *testing.T) {
		r := require.New(t)
		b, _ := newBlob([]byte("tampered blob content!!"))
		rc, err := b.ReadCloser()
		r.NoError(err)
		_, err = io.ReadAll(rc)
		r.NoError(err)
		r.Error(rc.Close())
	})
}
