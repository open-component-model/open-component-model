package client_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	_ "crypto/sha256"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/transfer/internal/repositoryupload/client"
)

type source struct {
	io.Reader
	closeErr error
	closes   int
}

func (s *source) Close() error {
	s.closes++
	return s.closeErr
}

func TestUploadBody_Close(t *testing.T) {
	for _, tc := range []struct {
		name     string
		size     int64
		limit    int64
		closeErr error
		wantErr  string
	}{
		{name: "unknown size", size: -1, limit: -1},
		{name: "known size without EOF", size: 7, limit: 7},
		{name: "partial unknown size", size: -1, limit: 3, wantErr: "incomplete upload"},
		{name: "partial known size", size: 7, limit: 3, wantErr: "incomplete upload"},
		{name: "close verification error", size: -1, limit: -1, closeErr: errors.New("checksum mismatch"), wantErr: "checksum mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			src := &source{Reader: strings.NewReader("content"), closeErr: tc.closeErr}
			body := client.NewUploadBody(src, tc.size, digest.SHA256)
			if tc.limit < 0 {
				_, err := io.Copy(io.Discard, body)
				r.NoError(err)
			} else {
				_, err := io.CopyN(io.Discard, body, tc.limit)
				r.NoError(err)
			}
			err := body.Close()
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				r.False(body.Complete())
			} else {
				r.NoError(err)
				r.True(body.Complete())
				r.Equal(digest.FromString("content"), body.Digest())
			}
			r.Equal(err, body.Close())
			r.Equal(1, src.closes)
			_, err = body.Read(make([]byte, 1))
			r.ErrorIs(err, io.ErrClosedPipe)
		})
	}
}

type blockingBlob struct {
	reader  *io.PipeReader
	started chan struct{}
}

func (b *blockingBlob) ReadCloser() (io.ReadCloser, error) { return b, nil }
func (b *blockingBlob) Read(p []byte) (int, error) {
	close(b.started)
	return b.reader.Read(p)
}
func (b *blockingBlob) Close() error { return b.reader.Close() }

func TestUploadBody_CloseUnblocksVerifiedSource(t *testing.T) {
	r := require.New(t)
	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close() })
	b := &blockingBlob{reader: pr, started: make(chan struct{})}
	verified, err := repository.VerifyDownload(t.Context(), &descriptor.Resource{Digest: &descriptor.Digest{
		HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digest.FromString("content").Encoded(),
	}}, b)
	r.NoError(err)
	rc, err := verified.ReadCloser()
	r.NoError(err)
	body := client.NewUploadBody(rc, -1, digest.SHA256)
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, body)
		done <- err
	}()
	<-b.started
	r.ErrorContains(body.Close(), "incomplete")
	r.Error(<-done)
	r.False(body.Complete())
}
