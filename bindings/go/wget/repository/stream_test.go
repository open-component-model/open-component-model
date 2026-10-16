package repository_test

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	_ "crypto/sha256"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/wget/repository"
)

// countingServer serves content with the given Content-Type and counts the requests.
func countingServer(t *testing.T, content []byte, contentType string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestDownloadResource_StreamsLazily asserts that DownloadResource returns a lazy
// blob for a GET request: the body streams on read, each read sends the request
// again, and no temporary file is written.
func TestDownloadResource_StreamsLazily(t *testing.T) {
	t.Parallel()
	content := []byte("streamed source without a temp file")

	t.Run("pinned media type sends no request before the first read", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		srv, hits := countingServer(t, content, "text/plain")
		tempDir := t.TempDir()
		repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir}, repository.WithHTTPClient(srv.Client()))

		resource := wgetResource(t, srv.URL, map[string]any{"mediaType": "application/x-pinned"})
		b, err := repo.DownloadResource(t.Context(), resource, nil)
		r.NoError(err)
		t.Cleanup(func() { closeBlob(b) })
		r.Equal(int64(0), hits.Load(), "no request may be sent before the blob is read")

		r.Equal(content, readBlob(t, b))
		r.Equal(int64(1), hits.Load())
		r.Equal(content, readBlob(t, b))
		r.Equal(int64(2), hits.Load(), "each read must send the request again")

		if sized, ok := b.(blob.SizeAware); ok {
			r.Equal(blob.SizeUnknown, sized.Size(), "a lazy blob does not know its size")
		}
		assertEmptyDir(t, tempDir)
	})

	t.Run("unknown media type is read from the response once", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		srv, hits := countingServer(t, content, "text/plain")
		repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(srv.Client()))

		b, err := repo.DownloadResource(t.Context(), wgetResource(t, srv.URL, map[string]any{}), nil)
		r.NoError(err)
		t.Cleanup(func() { closeBlob(b) })
		r.Equal(int64(1), hits.Load(), "the request is sent once to read the media type")

		mt, ok := b.(blob.MediaTypeAware)
		r.True(ok)
		mediaType, known := mt.MediaType()
		r.True(known)
		r.Equal("text/plain", mediaType)

		r.Equal(content, readBlob(t, b))
		r.Equal(int64(1), hits.Load(), "the first read must use the response that was already open")
	})
}

// TestDownloadResource_NonIdempotentIsDownloadedOnce asserts that a request which is
// not safe to repeat (a POST with a body) is downloaded once into a temporary file,
// so reading the blob again does not send the request again.
func TestDownloadResource_NonIdempotentIsDownloadedOnce(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	content := []byte("response to a POST")
	srv, hits := countingServer(t, content, "text/plain")
	tempDir := t.TempDir()
	repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir}, repository.WithHTTPClient(srv.Client()))

	resource := wgetResource(t, srv.URL, map[string]any{
		"verb": http.MethodPost,
		"body": base64.StdEncoding.EncodeToString([]byte("q=1")),
	})
	b, err := repo.DownloadResource(t.Context(), resource, nil)
	r.NoError(err)
	r.Equal(int64(1), hits.Load(), "the request is sent once, before return")

	r.Equal(content, readBlob(t, b))
	r.Equal(content, readBlob(t, b))
	r.Equal(int64(1), hits.Load(), "reading again must not send the request again")

	sized, ok := b.(blob.SizeAware)
	r.True(ok)
	r.Equal(int64(len(content)), sized.Size(), "the downloaded blob knows its size")

	entries, err := os.ReadDir(tempDir)
	r.NoError(err)
	r.NotEmpty(entries, "the content is held in a temporary file")
	closeBlob(b)
	assertEmptyDir(t, tempDir)
}

// TestDownloadResource_NonIdempotentChecksumPolicy asserts that on the download
// path of a request that is not safe to repeat, the checksum policy is verified
// before DownloadResource returns, and that a violation leaves no temporary file.
func TestDownloadResource_NonIdempotentChecksumPolicy(t *testing.T) {
	t.Parallel()
	content := []byte("response to a POST")
	tests := []struct {
		name       string
		advertised string
	}{
		{name: "advertised checksum mismatches", advertised: strings.Repeat("0", 64)},
		{name: "no checksum advertised"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.advertised != "" {
					w.Header().Set("X-Checksum-Sha256", tt.advertised)
				}
				_, _ = w.Write(content)
			}))
			t.Cleanup(srv.Close)

			tempDir := t.TempDir()
			repo := repository.NewResourceRepository(&filesystemv1alpha1.Config{TempFolder: &tempDir},
				repository.WithHTTPClient(srv.Client()),
				repository.WithChecksumConfig(&checksumhttpv1alpha1.Config{Mode: checksumhttpv1alpha1.ChecksumModeRequire}),
			)
			resource := wgetResource(t, srv.URL, map[string]any{
				"verb": http.MethodPost,
				"body": base64.StdEncoding.EncodeToString([]byte("q=1")),
			})

			_, err := repo.DownloadResource(t.Context(), resource, nil)
			r.ErrorContains(err, "checksum verification failed", "the policy must fail DownloadResource itself")
			assertEmptyDir(t, tempDir)
		})
	}
}

// TestDownloadResource_DigestMismatch is the stream-then-verify path at the
// repository level: the body streams in full, but the resource digest does not
// match, so the final read and close both fail.
func TestDownloadResource_DigestMismatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	const promised = "hello world"
	srv, _ := countingServer(t, []byte("not what was promised"), "")
	repo := repository.NewResourceRepository(nil, repository.WithHTTPClient(srv.Client()))
	resource := wgetResource(t, srv.URL, map[string]any{"mediaType": "text/plain"})
	resource.Digest = &descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  godigest.FromString(promised).Encoded(),
	}

	b, err := repo.DownloadResource(t.Context(), resource, nil)
	r.NoError(err)

	rc, err := b.ReadCloser()
	r.NoError(err)
	_, err = io.ReadAll(rc)
	r.ErrorContains(err, "digest mismatch")
	r.ErrorContains(rc.Close(), "digest mismatch")
}

// TestDownloadResource_ChecksumPolicyMismatch asserts the source-advertised
// checksum policy is enforced on the lazy path: a mismatching advertised checksum
// fails the stream once the body ends.
func TestDownloadResource_ChecksumPolicyMismatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Advertise a checksum that does not describe the served bytes.
		w.Header().Set("X-Checksum-Sha256", godigest.FromString("different content").Encoded())
		_, _ = w.Write([]byte("served bytes"))
	}))
	t.Cleanup(srv.Close)

	mode := checksumhttpv1alpha1.ChecksumModeRequire
	repo := repository.NewResourceRepository(nil,
		repository.WithHTTPClient(srv.Client()),
		repository.WithChecksumConfig(&checksumhttpv1alpha1.Config{Mode: mode}),
	)
	resource := wgetResource(t, srv.URL, map[string]any{"mediaType": "text/plain"})

	b, err := repo.DownloadResource(t.Context(), resource, nil)
	r.NoError(err)

	rc, err := b.ReadCloser()
	r.NoError(err)
	_, err = io.ReadAll(rc)
	r.ErrorContains(err, "checksum")
	r.Error(rc.Close())
}

func closeBlob(b blob.ReadOnlyBlob) {
	if c, ok := b.(io.Closer); ok {
		_ = c.Close()
	}
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "no temporary file may be left in %s", dir)
}
