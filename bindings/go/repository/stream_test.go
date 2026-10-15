package repository

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// lazySource mirrors a wget/s3 stream: every ReadCloser call is one source
// request, and the media type is only known after the first request unless it
// is pinned.
type lazySource struct {
	content    string
	pinned     string
	header     string
	idempotent bool
	openErr    error

	opens   int
	readers []*trackedReader
	seen    string
	closed  bool
}

func (s *lazySource) ReadCloser() (io.ReadCloser, error) {
	s.opens++
	if s.openErr != nil {
		return nil, s.openErr
	}
	s.seen = s.header
	rc := &trackedReader{Reader: strings.NewReader(s.content)}
	s.readers = append(s.readers, rc)
	return rc, nil
}

func (s *lazySource) MediaType() (string, bool) {
	if s.pinned != "" {
		return s.pinned, true
	}
	return s.seen, s.seen != ""
}

func (s *lazySource) Idempotent() bool { return s.idempotent }

func (s *lazySource) Close() error {
	s.closed = true
	return nil
}

// opaqueSource is a stream that does not report idempotency.
type opaqueSource struct{ opens int }

func (s *opaqueSource) ReadCloser() (io.ReadCloser, error) {
	s.opens++
	return io.NopCloser(strings.NewReader("opaque")), nil
}

type trackedReader struct {
	io.Reader
	closed bool
}

func (r *trackedReader) Close() error {
	r.closed = true
	return nil
}

type streamingRepo struct {
	ResourceRepository
	stream     blob.ReadOnlyBlob
	downloaded blob.ReadOnlyBlob
	downloads  int
}

func (r *streamingRepo) DownloadResourceStream(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	return r.stream, nil
}

func (r *streamingRepo) DownloadResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	r.downloads++
	return r.downloaded, nil
}

type plainRepo struct {
	ResourceRepository
	downloaded blob.ReadOnlyBlob
	downloads  int
}

func (r *plainRepo) DownloadResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	r.downloads++
	return r.downloaded, nil
}

func readAll(t *testing.T, b blob.ReadOnlyBlob) string {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	return string(data)
}

func TestStreamOrDownloadResource(t *testing.T) {
	res := &descriptor.Resource{}

	t.Run("repository without streaming downloads", func(t *testing.T) {
		r := require.New(t)
		downloaded := &opaqueSource{}
		repo := &plainRepo{downloaded: downloaded}

		b, streamed, err := StreamOrDownloadResource(t.Context(), repo, res, nil)
		r.NoError(err)
		r.False(streamed)
		r.Same(downloaded, b)
		r.Equal(1, repo.downloads)
	})

	t.Run("stream without idempotency report is used as is", func(t *testing.T) {
		r := require.New(t)
		stream := &opaqueSource{}
		repo := &streamingRepo{stream: stream}

		b, streamed, err := StreamOrDownloadResource(t.Context(), repo, res, nil)
		r.NoError(err)
		r.False(streamed)
		r.Same(stream, b)
		r.Zero(stream.opens)
		r.Zero(repo.downloads)
	})

	t.Run("verified download from a registry fallback is not downloaded again", func(t *testing.T) {
		r := require.New(t)
		verified, err := VerifyDownload(t.Context(), &descriptor.Resource{Digest: &descriptor.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "genericBlobDigest/v1",
			Value:                  digest.FromString("opaque").Encoded(),
		}}, &opaqueSource{})
		r.NoError(err)
		repo := &streamingRepo{stream: verified}

		b, streamed, err := StreamOrDownloadResource(t.Context(), repo, res, nil)
		r.NoError(err)
		r.False(streamed)
		r.Same(verified, b)
		r.Zero(repo.downloads)
	})

	t.Run("non-idempotent stream is downloaded instead", func(t *testing.T) {
		r := require.New(t)
		stream := &lazySource{idempotent: false}
		downloaded := &opaqueSource{}
		repo := &streamingRepo{stream: stream, downloaded: downloaded}

		b, streamed, err := StreamOrDownloadResource(t.Context(), repo, res, nil)
		r.NoError(err)
		r.False(streamed)
		r.Same(downloaded, b)
		r.Zero(stream.opens, "the non-idempotent request must not be sent by the stream")
		r.True(stream.closed)
		r.Equal(1, repo.downloads)
	})

	t.Run("stream with pinned media type stays lazy", func(t *testing.T) {
		r := require.New(t)
		stream := &lazySource{content: "data", pinned: "text/plain", idempotent: true}
		repo := &streamingRepo{stream: stream}

		b, streamed, err := StreamOrDownloadResource(t.Context(), repo, res, nil)
		r.NoError(err)
		r.True(streamed)
		r.Same(stream, b)
		r.Zero(stream.opens)
	})

	t.Run("stream with unknown media type is opened once", func(t *testing.T) {
		r := require.New(t)
		stream := &lazySource{content: "data", header: "text/plain", idempotent: true}
		repo := &streamingRepo{stream: stream}

		b, streamed, err := StreamOrDownloadResource(t.Context(), repo, res, nil)
		r.NoError(err)
		r.True(streamed)
		r.Equal(1, stream.opens)

		mt, ok := b.(blob.MediaTypeAware)
		r.True(ok)
		got, known := mt.MediaType()
		r.True(known)
		r.Equal("text/plain", got)

		idem, ok := b.(IdempotentSource)
		r.True(ok)
		r.True(idem.Idempotent())

		r.Equal("data", readAll(t, b))
		r.Equal(1, stream.opens, "the first read must reuse the open request")
		r.Equal("data", readAll(t, b))
		r.Equal(2, stream.opens, "later reads open the source again")

		closer, ok := b.(io.Closer)
		r.True(ok)
		r.NoError(closer.Close())
		r.True(stream.closed)
	})

	t.Run("close releases a reader that was never read", func(t *testing.T) {
		r := require.New(t)
		stream := &lazySource{content: "data", idempotent: true}
		repo := &streamingRepo{stream: stream}

		b, _, err := StreamOrDownloadResource(t.Context(), repo, res, nil)
		r.NoError(err)
		r.Len(stream.readers, 1)

		closer, ok := b.(io.Closer)
		r.True(ok)
		r.NoError(closer.Close())
		r.True(stream.readers[0].closed)
		r.True(stream.closed)
	})

	t.Run("open error is returned", func(t *testing.T) {
		r := require.New(t)
		openErr := errors.New("boom")
		stream := &lazySource{idempotent: true, openErr: openErr}
		repo := &streamingRepo{stream: stream}

		_, _, err := StreamOrDownloadResource(t.Context(), repo, res, nil)
		r.ErrorIs(err, openErr)
		r.True(stream.closed)
	})
}
