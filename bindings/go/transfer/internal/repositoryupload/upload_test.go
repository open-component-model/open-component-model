package repositoryupload

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	uploadv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/transformation/spec/v1alpha1"
)

// lazyMediaTypeBlob is a streaming source blob whose media type (the response
// Content-Type) is only known once its stream is opened, like the wget/s3 lazy
// blobs. MediaType reports unknown until the first ReadCloser call primes it. It
// is idempotent, modelling a GET source that may be opened more than once.
type lazyMediaTypeBlob struct {
	content     []byte
	contentType string
	primed      bool
	opens       int
}

func (b *lazyMediaTypeBlob) ReadCloser() (io.ReadCloser, error) {
	b.primed = true
	b.opens++
	return io.NopCloser(bytes.NewReader(b.content)), nil
}

func (b *lazyMediaTypeBlob) MediaType() (string, bool) {
	if !b.primed {
		return "", false
	}
	return b.contentType, true
}

func (b *lazyMediaTypeBlob) Idempotent() bool { return true }

// nonIdempotentBlob is a streaming source blob whose request must not be repeated
// (for example a wget access with a non-GET verb and a body). It records every
// open so a test can assert the streaming path never issued the request.
type nonIdempotentBlob struct {
	opens int
}

func (b *nonIdempotentBlob) ReadCloser() (io.ReadCloser, error) {
	b.opens++
	return io.NopCloser(bytes.NewReader(nil)), nil
}

func (b *nonIdempotentBlob) MediaType() (string, bool) { return "", false }

func (b *nonIdempotentBlob) Idempotent() bool { return false }

// streamingStubRepository streams StreamBlob as the source and derives no
// credential identity, exercising the uploader streaming path. DownloadResource
// returns the materialized MaterializedBlob and records the call, so a test can
// assert the non-idempotent fallback.
type streamingStubRepository struct {
	repository.ResourceRepository
	StreamBlob        blob.ReadOnlyBlob
	MaterializedBlob  blob.ReadOnlyBlob
	downloadResources int
}

func (s *streamingStubRepository) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (s *streamingStubRepository) DownloadResourceStream(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	return s.StreamBlob, nil
}

func (s *streamingStubRepository) DownloadResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	s.downloadResources++
	return s.MaterializedBlob, nil
}

// TestSourcePrimesStreamingMediaType guards the primeMediaType/downloadRemoteSource
// path: a lazy streaming source without a pinned media type must still upload with
// the response Content-Type, not the application/octet-stream fallback.
func TestSourcePrimesStreamingMediaType(t *testing.T) {
	r := require.New(t)
	src := &lazyMediaTypeBlob{content: []byte("hello"), contentType: "text/plain"}
	u := &Uploader{ResourceRepository: &streamingStubRepository{StreamBlob: src}}
	spec := &uploadv1alpha1.RepositoryUploadSpec{
		ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "c", Version: "v"},
	}

	b, mediaType, err := u.source(t.Context(), spec, &descriptor.Resource{})
	r.NoError(err)
	r.NotNil(b)
	r.Equal("text/plain", mediaType, "the response Content-Type is primed, not the octet-stream fallback")
	r.NotEqual(octetStream, mediaType)
}

// TestSourceMaterializesNonIdempotentStream guards the idempotency gate: a
// streaming source whose request is not idempotent must be materialized with
// DownloadResource and must never be opened by the streaming path, so the
// non-idempotent request is issued only once (by DownloadResource).
func TestSourceMaterializesNonIdempotentStream(t *testing.T) {
	r := require.New(t)
	stream := &nonIdempotentBlob{}
	materialized := &lazyMediaTypeBlob{content: []byte("hi"), contentType: "text/plain", primed: true}
	repo := &streamingStubRepository{StreamBlob: stream, MaterializedBlob: materialized}
	u := &Uploader{ResourceRepository: repo}
	spec := &uploadv1alpha1.RepositoryUploadSpec{
		ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "c", Version: "v"},
	}

	b, mediaType, err := u.source(t.Context(), spec, &descriptor.Resource{})
	r.NoError(err)
	r.NotNil(b)
	r.Equal(0, stream.opens, "the non-idempotent streaming source must never be opened")
	r.Equal(1, repo.downloadResources, "the non-idempotent source must be materialized once")
	r.Equal("text/plain", mediaType)
}

func TestPoll(t *testing.T) {
	errTry := errors.New("try failed")
	tests := []struct {
		name      string
		doneAt    int // call that reports done; 0 never
		failAt    int // call that fails; 0 never
		cancel    bool
		wantDone  bool
		wantCalls int
		wantErr   error
	}{
		{name: "done on the third try", doneAt: 3, wantDone: true, wantCalls: 3},
		{name: "never done", wantCalls: PollAttempts},
		{name: "a failing try ends polling", failAt: 2, wantCalls: 2, wantErr: errTry},
		{name: "a cancelled context ends polling", cancel: true, wantCalls: 1, wantErr: context.Canceled},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			interval := time.Microsecond
			if tc.cancel {
				cancel()
				interval = time.Hour
			}
			calls := 0
			done, err := Poll(ctx, interval, func() (bool, error) {
				calls++
				if calls == tc.failAt {
					return false, errTry
				}
				return calls == tc.doneAt, nil
			})
			r.ErrorIs(err, tc.wantErr)
			r.Equal(tc.wantDone, done)
			r.Equal(tc.wantCalls, calls)
		})
	}
}
