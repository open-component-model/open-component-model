package repository

import (
	"context"
	"io"
	"os"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
)

// TestDownloadResource_StreamsLazily asserts that DownloadResource returns a lazy
// blob: the object body streams on read, each read sends GetObject again, and no
// temporary file is written.
func TestDownloadResource_StreamsLazily(t *testing.T) {
	content := []byte("hello from s3 stream")

	t.Run("pinned media type sends no request before the first read", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeS3(t, content, "")
		tempDir := t.TempDir()
		repository := NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir})

		resource := s3Resource(servedBy(srv, &v2.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt", MediaType: "text/plain"}))
		b, err := repository.DownloadResource(context.Background(), resource, fakeCredentials())
		r.NoError(err)
		r.Empty(srv.recorded(), "no request may be sent before the blob is read")

		r.Equal(content, readStream(t, b))
		r.Equal(content, readStream(t, b))
		r.Len(srv.recorded(), 2, "each read must send GetObject again")

		entries, err := os.ReadDir(tempDir)
		r.NoError(err)
		r.Empty(entries, "the object must not be written into the temp folder")
	})

	t.Run("unknown media type is read from the response once", func(t *testing.T) {
		r := require.New(t)
		srv := newFakeS3(t, content, "")
		repository := NewResourceRepository(nil)

		resource := s3Resource(servedBy(srv, &v2.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt"}))
		b, err := repository.DownloadResource(context.Background(), resource, fakeCredentials())
		r.NoError(err)
		r.Len(srv.recorded(), 1, "the object is requested once to read the media type")

		r.Equal(content, readStream(t, b))
		r.Len(srv.recorded(), 1, "the first read must use the response that was already open")
	})
}

// TestDownloadResource_DigestMismatch is the stream-then-verify path at the
// repository level: the object streams in full, but the resource digest does not
// match, so the final read and close both fail.
func TestDownloadResource_DigestMismatch(t *testing.T) {
	r := require.New(t)

	srv := newFakeS3(t, []byte("not what was promised"), "")
	repository := NewResourceRepository(nil)

	resource := s3Resource(servedBy(srv, &v2.S3{BucketName: "b", ObjectKey: "k", MediaType: "text/plain"}))
	resource.Digest = &descriptor.Digest{
		HashAlgorithm:          hashAlgorithmSHA256,
		NormalisationAlgorithm: genericBlobDigestV1,
		Value:                  godigest.FromString("hello world").Encoded(),
	}

	b, err := repository.DownloadResource(context.Background(), resource, fakeCredentials())
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
