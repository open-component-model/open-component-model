package repository

import (
	"context"
	"io"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	repo "ocm.software/open-component-model/bindings/go/repository"
	v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
)

// TestDownloadResourceStream_CapabilityAndStreaming asserts the s3 repository offers
// the streaming capability and that the lazy blob is replayable, re-issuing
// GetObject on each read without a temporary file.
func TestDownloadResourceStream_CapabilityAndStreaming(t *testing.T) {
	r := require.New(t)

	content := []byte("hello from s3 stream")
	srv := newFakeS3(t, content, "")
	repository := NewResourceRepository(nil)

	// The repository must satisfy the streaming capability interface.
	var streamer repo.StreamingResourceRepository = repository

	resource := s3Resource(servedBy(srv, &v2.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt"}))
	b, err := streamer.DownloadResourceStream(context.Background(), resource, fakeCredentials())
	r.NoError(err)
	r.NotNil(b)

	r.Equal(content, readStream(t, b))
	r.Equal(content, readStream(t, b))
	r.Len(srv.recorded(), 2, "each read must re-issue GetObject")
}

// TestDownloadResourceStream_DigestMismatch is the stream-then-verify path at the
// repository level: the object streams in full, but the resource digest does not
// match, so the final read and close both fail.
func TestDownloadResourceStream_DigestMismatch(t *testing.T) {
	r := require.New(t)

	srv := newFakeS3(t, []byte("not what was promised"), "")
	repository := NewResourceRepository(nil)

	resource := s3Resource(servedBy(srv, &v2.S3{BucketName: "b", ObjectKey: "k"}))
	resource.Digest = &descriptor.Digest{
		HashAlgorithm:          hashAlgorithmSHA256,
		NormalisationAlgorithm: genericBlobDigestV1,
		Value:                  godigest.FromString("hello world").Encoded(),
	}

	b, err := repository.DownloadResourceStream(context.Background(), resource, fakeCredentials())
	r.NoError(err)

	rc, err := b.ReadCloser()
	r.NoError(err)
	_, err = io.ReadAll(rc)
	r.ErrorContains(err, "digest mismatch")
	r.ErrorContains(rc.Close(), "digest mismatch")
}

// readStream fully reads a blob and returns its bytes.
func readStream(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}
