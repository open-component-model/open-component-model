package oci

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

func TestFetchLayer(t *testing.T) {
	readErr := errors.New("read failed")
	closeErr := errors.New("close failed")
	fetchErr := errors.New("fetch failed")
	for _, tt := range []struct {
		name         string
		data         string
		size         int64
		digest       digest.Digest
		mediaType    string
		readErr      error
		closeErr     error
		fetchErr     error
		wantReadErr  string
		wantCause    error
		wantCloseErr error
	}{
		{name: "layer", data: "layer", size: 5, mediaType: ocispec.MediaTypeImageLayer},
		{name: "default media type", data: "layer", size: 5},
		{name: "empty layer", size: 0},
		{name: "size too large", data: "layer", size: 6, wantReadErr: "size mismatch: expected 6 bytes, read 5"},
		{name: "size too small", data: "layer", size: 4, wantReadErr: "size mismatch: expected 4 bytes, read 5"},
		{name: "zero size with content", data: "layer", size: 0, wantReadErr: "size mismatch: expected 0 bytes"},
		{name: "incorrect digest", data: "layer", size: 5, digest: digest.FromString("other"), wantReadErr: "digest mismatch"},
		{name: "valid prefix with extra content", data: "layer extra", size: 5, digest: digest.FromString("layer"), wantReadErr: "size mismatch"},
		{name: "read error", data: "layer", size: 5, readErr: readErr, wantCause: readErr},
		{name: "close error", data: "layer", size: 5, closeErr: closeErr, wantCloseErr: closeErr},
		{name: "fetch error", fetchErr: fetchErr, wantReadErr: "failed to fetch layer", wantCause: fetchErr},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			store := &layerStorage{data: tt.data, readErr: tt.readErr, closeErr: tt.closeErr, fetchErr: tt.fetchErr}
			dig := tt.digest
			if dig == "" {
				dig = digest.FromString(tt.data)
			}
			desc := ocispec.Descriptor{Digest: dig, Size: tt.size, MediaType: tt.mediaType}

			b := fetchLayer(t.Context(), store, desc)
			r.Empty(store.readers, "the layer must not be fetched before it is read")

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

			for i := range 2 {
				rc, err := b.ReadCloser()
				if tt.fetchErr != nil {
					r.ErrorContains(err, tt.wantReadErr)
					r.ErrorIs(err, tt.wantCause)
					return
				}
				r.NoError(err)
				data, err := io.ReadAll(rc)
				closeErr := rc.Close()
				r.Len(store.readers, i+1, "every read fetches the layer anew")
				r.True(store.readers[i].closed)
				switch {
				case tt.wantReadErr != "":
					r.ErrorContains(err, tt.wantReadErr)
					r.Error(closeErr)
				case tt.wantCause != nil:
					r.ErrorIs(err, tt.wantCause)
				case tt.wantCloseErr != nil:
					r.NoError(err)
					r.ErrorIs(closeErr, tt.wantCloseErr)
				default:
					r.NoError(err)
					r.NoError(closeErr)
					r.Equal(tt.data, string(data))
				}
			}
		})
	}
}

// layerStorage serves a fresh reader over data on every fetch.
type layerStorage struct {
	data     string
	readErr  error
	closeErr error
	fetchErr error
	readers  []*layerReader
}

func (s *layerStorage) Fetch(context.Context, ocispec.Descriptor) (io.ReadCloser, error) {
	if s.fetchErr != nil {
		return nil, s.fetchErr
	}
	reader := &layerReader{Reader: strings.NewReader(s.data), readErr: s.readErr, closeErr: s.closeErr}
	s.readers = append(s.readers, reader)
	return reader, nil
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
