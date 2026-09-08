package stream

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"

	"ocm.software/open-component-model/bindings/go/blob"
)

func TestOCILayerResourceStream_Materialize(t *testing.T) {
	readErr := errors.New("read failed")
	closeErr := errors.New("close failed")
	fetchErr := errors.New("fetch failed")
	for _, tt := range []struct {
		name      string
		data      string
		size      int64
		digest    digest.Digest
		mediaType string
		readErr   error
		closeErr  error
		fetchErr  error
		wantErr   string
		wantCause error
	}{
		{name: "layer", data: "layer", size: 5, mediaType: ocispec.MediaTypeImageLayer},
		{name: "default media type", data: "layer", size: 5},
		{name: "empty layer", size: 0},
		{name: "size too large", data: "layer", size: 6, wantErr: "the descriptor declares 6"},
		{name: "size too small", data: "layer", size: 4, wantErr: "the descriptor declares 4"},
		{name: "zero size with content", data: "layer", size: 0, wantErr: "the descriptor declares 0"},
		{name: "incorrect digest", data: "layer", size: 5, digest: digest.FromString("other"), wantErr: "differed from loaded digest"},
		{name: "valid prefix with extra content", data: "layer extra", size: 5, digest: digest.FromString("layer"), wantErr: "differed from loaded digest"},
		{name: "read error", readErr: readErr, wantErr: "failed to read layer", wantCause: readErr},
		{name: "close error", data: "layer", size: 5, closeErr: closeErr, wantCause: closeErr},
		{name: "read and close errors", readErr: readErr, closeErr: closeErr, wantCause: readErr},
		{name: "fetch error", fetchErr: fetchErr, wantErr: "failed to fetch layer", wantCause: fetchErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			reader := &layerReader{Reader: strings.NewReader(tt.data), readErr: tt.readErr, closeErr: tt.closeErr}
			store := &layerStorage{reader: reader, fetchErr: tt.fetchErr}
			dig := tt.digest
			if dig == "" {
				dig = digest.FromString(tt.data)
			}
			s := &OCILayerResourceStream{
				ReadOnlyGraphStorage: store,
				Descriptor:           ocispec.Descriptor{Digest: dig, Size: tt.size, MediaType: tt.mediaType},
			}
			r.Equal(s.Descriptor, s.Root())
			r.False(reader.closed)

			b, err := s.Materialize(t.Context())
			r.Equal(tt.fetchErr == nil, reader.closed)
			if tt.wantErr != "" || tt.wantCause != nil {
				if tt.wantErr != "" {
					r.ErrorContains(err, tt.wantErr)
				}
				if tt.wantCause != nil {
					r.ErrorIs(err, tt.wantCause)
				}
				if tt.closeErr != nil {
					r.ErrorIs(err, tt.closeErr)
				}
				return
			}
			r.NoError(err)
			r.Equal(tt.size, b.(blob.SizeAware).Size())
			actualDigest, ok := b.(blob.DigestAware).Digest()
			r.True(ok)
			r.Equal(dig.String(), actualDigest)
			mediaType, ok := b.(blob.MediaTypeAware).MediaType()
			r.True(ok)
			if tt.mediaType == "" {
				r.Equal("application/octet-stream", mediaType)
			} else {
				r.Equal(tt.mediaType, mediaType)
			}
			for range 2 {
				rc, err := b.ReadCloser()
				r.NoError(err)
				data, err := io.ReadAll(rc)
				r.NoError(err)
				r.NoError(rc.Close())
				r.Equal(tt.data, string(data))
			}
		})
	}
}

type layerStorage struct {
	content.ReadOnlyGraphStorage
	reader   io.ReadCloser
	fetchErr error
}

func (s *layerStorage) Fetch(context.Context, ocispec.Descriptor) (io.ReadCloser, error) {
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	return s.reader, nil
}

type layerReader struct {
	io.Reader
	closed   bool
	readErr  error
	closeErr error
}

func (r *layerReader) Read(p []byte) (int, error) {
	if r.closed {
		return 0, errors.New("read after close")
	}
	if r.readErr != nil {
		return 0, r.readErr
	}
	return r.Reader.Read(p)
}

func (r *layerReader) Close() error {
	r.closed = true
	return r.closeErr
}
