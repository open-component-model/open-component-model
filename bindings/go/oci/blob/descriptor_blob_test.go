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

func TestDescriptorBlobClose(t *testing.T) {
	data := []byte("descriptor blob content")
	desc := ocispec.Descriptor{Digest: digest.FromBytes(data), Size: int64(len(data))}

	t.Run("closing an unread blob releases the content", func(t *testing.T) {
		r := require.New(t)
		src := &closeCountingReader{Reader: bytes.NewReader(data)}
		r.NoError(ociblob.NewDescriptorBlob(src, desc).Close())
		r.Equal(1, src.closes)
	})

	t.Run("a partially read reader releases the content although verification fails", func(t *testing.T) {
		r := require.New(t)
		src := &closeCountingReader{Reader: bytes.NewReader(data)}
		b := ociblob.NewDescriptorBlob(src, desc)
		rc, err := b.ReadCloser()
		r.NoError(err)
		_, err = rc.Read(make([]byte, 4))
		r.NoError(err)
		r.Error(rc.Close())
		r.Equal(1, src.closes)
		r.NoError(b.Close(), "closing the blob after its reader must not close the content again")
		r.Equal(1, src.closes)
	})
}
