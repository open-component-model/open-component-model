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
// blobs. MediaType reports unknown until the first ReadCloser call primes it.
type lazyMediaTypeBlob struct {
	content     []byte
	contentType string
	primed      bool
}

func (b *lazyMediaTypeBlob) ReadCloser() (io.ReadCloser, error) {
	b.primed = true
	return io.NopCloser(bytes.NewReader(b.content)), nil
}

func (b *lazyMediaTypeBlob) MediaType() (string, bool) {
	if !b.primed {
		return "", false
	}
	return b.contentType, true
}

// streamingStubRepository streams Blob as the source and derives no credential
// identity, exercising the uploader streaming path.
type streamingStubRepository struct {
	repository.ResourceRepository
	Blob blob.ReadOnlyBlob
}

func (s *streamingStubRepository) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (s *streamingStubRepository) DownloadResourceStream(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	return s.Blob, nil
}

// TestSourcePrimesStreamingMediaType guards the primeMediaType/downloadRemoteSource
// path: a lazy streaming source without a pinned media type must still upload with
// the response Content-Type, not the application/octet-stream fallback.
func TestSourcePrimesStreamingMediaType(t *testing.T) {
	r := require.New(t)
	src := &lazyMediaTypeBlob{content: []byte("hello"), contentType: "text/plain"}
	u := &Uploader{ResourceRepository: &streamingStubRepository{Blob: src}}
	spec := &uploadv1alpha1.RepositoryUploadSpec{
		ComponentVersion: &uploadv1alpha1.RepositoryUploadComponentVersion{Component: "c", Version: "v"},
	}

	b, mediaType, err := u.source(t.Context(), spec, &descriptor.Resource{})
	r.NoError(err)
	r.NotNil(b)
	r.Equal("text/plain", mediaType, "the response Content-Type is primed, not the octet-stream fallback")
	r.NotEqual(octetStream, mediaType)
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
