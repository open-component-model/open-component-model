package repository_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	_ "crypto/sha256"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	checksumhttpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/checksum/http/v1alpha1/spec"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	repo "ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/wget/repository"
)

// TestDownloadResourceStream_CapabilityAndStreaming asserts the wget repository
// exposes the streaming capability and that the lazy blob is replayable, re-issuing
// the request on each read without writing a temporary file.
func TestDownloadResourceStream_CapabilityAndStreaming(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	content := []byte("streamed source without a temp file")
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(content)
	}))
	t.Cleanup(server.Close)

	repository := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
	// The repository must satisfy the streaming capability interface.
	var streamer repo.StreamingResourceRepository = repository

	resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})
	b, err := streamer.DownloadResourceStream(t.Context(), resource, nil)
	r.NoError(err)
	r.NotNil(b)

	r.Equal(content, readBlob(t, b))
	r.Equal(content, readBlob(t, b))
	r.Equal(int64(2), hits.Load(), "each read must re-issue the request")

	mt, ok := b.(blob.MediaTypeAware)
	r.True(ok)
	mediaType, known := mt.MediaType()
	r.True(known)
	r.Equal("text/plain", mediaType)
}

// TestDownloadResourceStream_DigestMismatch is the stream-then-verify path at the
// repository level: the body streams in full, but the resource digest does not
// match, so the final read and close both fail.
func TestDownloadResourceStream_DigestMismatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	const promised = "hello world"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not what was promised"))
	}))
	t.Cleanup(server.Close)

	repository := repository.NewResourceRepository(nil, repository.WithHTTPClient(server.Client()))
	resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})
	resource.Digest = &descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  godigest.FromString(promised).Encoded(),
	}

	b, err := repository.DownloadResourceStream(t.Context(), resource, nil)
	r.NoError(err)

	rc, err := b.ReadCloser()
	r.NoError(err)
	_, err = io.ReadAll(rc)
	r.ErrorContains(err, "digest mismatch")
	r.ErrorContains(rc.Close(), "digest mismatch")
}

// TestDownloadResourceStream_ChecksumPolicyMismatch asserts the source-advertised
// checksum policy is preserved inline on the streaming path: a mismatching
// advertised checksum fails the stream once the body ends.
func TestDownloadResourceStream_ChecksumPolicyMismatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Advertise a checksum that does not describe the served bytes.
		w.Header().Set("x-checksum-sha256", godigest.FromString("different content").Encoded())
		_, _ = w.Write([]byte("served bytes"))
	}))
	t.Cleanup(server.Close)

	require := checksumhttpv1alpha1.ChecksumModeRequire
	repository := repository.NewResourceRepository(nil,
		repository.WithHTTPClient(server.Client()),
		repository.WithChecksumConfig(&checksumhttpv1alpha1.Config{Mode: require}),
	)
	resource := wgetResource(t, server.URL, map[string]any{"url": server.URL + "/resource"})

	b, err := repository.DownloadResourceStream(t.Context(), resource, nil)
	r.NoError(err)

	rc, err := b.ReadCloser()
	r.NoError(err)
	_, err = io.ReadAll(rc)
	r.ErrorContains(err, "checksum")
	r.Error(rc.Close())
}
