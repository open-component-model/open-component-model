package integration_test

import (
	"io"
	"net/http"
	"os"
	"sync/atomic"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	s3repo "ocm.software/open-component-model/bindings/go/s3/repository"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	accessv2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

// countingRoundTripper counts every HTTP request issued through it, so a test can
// observe that a lazy stream sends no request until it is read and sends a fresh
// request on every replay.
type countingRoundTripper struct {
	next  http.RoundTripper
	count atomic.Int64
}

func (c *countingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	c.count.Add(1)
	return c.next.RoundTrip(req)
}

func (c *countingRoundTripper) requests() int64 { return c.count.Load() }

// Test_Integration_S3Stream exercises the lazy DownloadResource of the s3 repository
// end to end against a RustFS container and the real AWS SDK: it proves that the
// returned blob defers all I/O until it is read, sends GetObject again on replay, and
// never writes a temporary file.
//
// The stream-then-verify digest-mismatch path is covered by the repository unit test
// (TestDownloadResource_DigestMismatch); this test focuses on the behaviours that only
// a real store and the real SDK can confirm: laziness, replay and no temp file.
func Test_Integration_S3Stream(t *testing.T) {
	ctx := t.Context()

	hostPort := startRustFS(t, ctx)
	endpoint := "http://" + hostPort

	setup := newSetupClient(t, ctx, endpoint, rustfsAccessKey, rustfsSecretKey)

	creds := &credv1.S3Credentials{
		Type:            credv1.S3CredentialsVersionedType,
		AccessKeyID:     rustfsAccessKey,
		SecretAccessKey: rustfsSecretKey,
	}

	access := func(bucket, key string) *accessv2.S3 {
		return &accessv2.S3{
			Type:         accessspec.V2VersionedType,
			Region:       "us-east-1",
			BucketName:   bucket,
			ObjectKey:    key,
			Endpoint:     endpoint,
			UsePathStyle: true,
			MediaType:    "text/plain",
		}
	}
	resourceFor := func(a *accessv2.S3, content []byte) *descriptor.Resource {
		res := &descriptor.Resource{}
		res.Access = a
		res.Digest = &descriptor.Digest{
			HashAlgorithm: "SHA-256",
			Value:         godigest.FromBytes(content).Encoded(),
		}
		return res
	}

	// newRepo builds a repository whose downloads flow through a request counter, with
	// retries disabled so request counts are deterministic.
	newRepo := func(tempDir string) (*s3repo.ResourceRepository, *countingRoundTripper) {
		counter := &countingRoundTripper{next: http.DefaultTransport}
		fsConfig := &filesystemv1alpha1.Config{TempFolder: &tempDir}
		httpConfig := &httpv1alpha1.Config{
			// -1 disables SDK retries, so one GetObject means exactly one request.
			Retry: &httpv1alpha1.RetryConfig{MaxRetries: new(-1)},
		}
		repo := s3repo.NewResourceRepository(fsConfig,
			s3repo.WithHTTPClient(&http.Client{Transport: counter}),
			s3repo.WithHTTPConfig(httpConfig))
		return repo, counter
	}

	t.Run("streams lazily, replays, and verifies", func(t *testing.T) {
		r := require.New(t)
		const bucket, key = "stream-bucket", "path/to/blob.txt"
		content := []byte("hello streaming ocm from s3")
		createBucket(t, ctx, setup, bucket)
		putObject(t, ctx, setup, bucket, key, content)

		repo, counter := newRepo(t.TempDir())
		res := resourceFor(access(bucket, key), content)

		b, err := repo.DownloadResource(ctx, res, creds)
		r.NoError(err)
		t.Cleanup(func() {
			if c, ok := b.(io.Closer); ok {
				_ = c.Close()
			}
		})

		// Lazy: the media type is pinned up front, so DownloadResource does not open
		// the object to read it. No request has been sent yet.
		r.Equal(int64(0), counter.requests(), "no request may be sent before the stream is read")

		// First read issues exactly one request and returns the full content.
		rc, err := b.ReadCloser()
		r.NoError(err)
		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.NoError(rc.Close())
		r.Equal(content, got)
		r.Equal(int64(1), counter.requests(), "the first read must issue exactly one request")

		// Replay: a second open re-issues the request from the beginning.
		rc2, err := b.ReadCloser()
		r.NoError(err)
		got2, err := io.ReadAll(rc2)
		r.NoError(err)
		r.NoError(rc2.Close())
		r.Equal(content, got2)
		r.Equal(int64(2), counter.requests(), "replaying the stream must issue a fresh request")
	})

	t.Run("streaming materializes no temporary file", func(t *testing.T) {
		r := require.New(t)
		const bucket, key = "stream-nofile-bucket", "blob.txt"
		content := []byte("streamed straight from the store, never written to disk")
		createBucket(t, ctx, setup, bucket)
		putObject(t, ctx, setup, bucket, key, content)

		tempDir := t.TempDir()
		repo, _ := newRepo(tempDir)
		res := resourceFor(access(bucket, key), content)

		// Stream the whole body, then assert the temp folder stayed empty.
		b, err := repo.DownloadResource(ctx, res, creds)
		r.NoError(err)
		rc, err := b.ReadCloser()
		r.NoError(err)
		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.NoError(rc.Close())
		r.Equal(content, got)
		if c, ok := b.(io.Closer); ok {
			r.NoError(c.Close())
		}

		entries, err := os.ReadDir(tempDir)
		r.NoError(err)
		r.Empty(entries, "streaming must not write any temporary file")
	})
}
