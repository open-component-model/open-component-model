package transformer

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// This file covers the two fused LocalBlob -> OCI registry streaming paths that differ
// only by whether the source digest is known before the fetch:
//
//   - (digest unknown): the content streams into the OCI chunked upload
//     (POST/PATCH/PUT) and the digest is computed on the fly.
//   - (digest known): the content streams into the regular oras. Push and the
//     known digest is carried through and verified; the chunked streaming push is skipped.
//

// methodRecordingTransport counts the HTTP methods issued through it so a test can
// prove whether the OCI chunked blob upload protocol (POST to start, PATCH per chunk,
// PUT to finalize) was used instead of a single monolithic PUT.
type methodRecordingTransport struct {
	base    http.RoundTripper
	mu      sync.Mutex
	methods map[string]int
}

func (t *methodRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.methods[req.Method]++
	t.mu.Unlock()
	return t.base.RoundTrip(req)
}

func (t *methodRecordingTransport) count(method string) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.methods[method]
}

// knownDigestSizeBlob is an idempotent, media-type-aware lazy source that ALSO
// exposes its size and digest up front, modeling a source whose digest is known
// before the fetch (for example an OCI local blob). pack routes such a blob to the
// regular oras.Push instead of the chunked streaming push.
type knownDigestSizeBlob struct {
	data      string
	mediaType string
}

var (
	_ blob.ReadOnlyBlob           = (*knownDigestSizeBlob)(nil)
	_ blob.SizeAware              = (*knownDigestSizeBlob)(nil)
	_ blob.DigestAware            = (*knownDigestSizeBlob)(nil)
	_ blob.MediaTypeAware         = (*knownDigestSizeBlob)(nil)
	_ repository.IdempotentSource = (*knownDigestSizeBlob)(nil)
)

func (b *knownDigestSizeBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(b.data)), nil
}
func (b *knownDigestSizeBlob) Size() int64 { return int64(len(b.data)) }
func (b *knownDigestSizeBlob) Digest() (string, bool) {
	return godigest.FromString(b.data).String(), true
}
func (b *knownDigestSizeBlob) MediaType() (string, bool) { return b.mediaType, true }
func (b *knownDigestSizeBlob) Idempotent() bool          { return true }

// startStreamTargetRegistry starts a real OCI registry (anonymous push, validation off
// like the chunked push test) and returns its host:port address.
func startStreamTargetRegistry(t *testing.T) string {
	t.Helper()
	r := require.New(t)
	runCtx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	registryContainer, err := registry.Run(runCtx, "registry:3.0.0",
		testcontainers.WithEnv(map[string]string{"REGISTRY_VALIDATION_DISABLED": "true"}),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(registryContainer)) })
	address, err := registryContainer.HostAddress(t.Context())
	r.NoError(err)
	return address
}

// Test_Integration_StreamLocalResource_ChunkedPush_DigestUnknown covers the fused
// LocalBlob -> OCI registry path when the source digest is NOT known before the
// fetch. A size-unknown, digest-unknown lazy source is streamed straight into a real
// registry via the OCI chunked upload protocol (POST/PATCH/PUT), the content digest is
// computed on the fly during the stream, and no temporary file is created.
func Test_Integration_StreamLocalResource_ChunkedPush_DigestUnknown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping chunked-push transfer integration test in short mode")
	}
	r := require.New(t)
	ctx := t.Context()

	address := startStreamTargetRegistry(t)

	// Build a real *oci.Repository whose client records HTTP methods, with chunked push
	// enabled at a small chunk size so a modest blob spans several PATCH requests.
	const chunkSize = 1024
	rec := &methodRecordingTransport{base: http.DefaultTransport.(*http.Transport).Clone(), methods: map[string]int{}}
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(address),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(&http.Client{Transport: rec}),
		urlresolver.WithChunkedPush(chunkSize, 1),
	)
	r.NoError(err)

	tempDir := t.TempDir()
	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(tempDir))
	r.NoError(err)

	// A size-unknown, idempotent lazy source whose descriptor carries NO digest: the
	// digest-unknown case. Content is larger than one chunk so chunking is observable.
	content := strings.Repeat("chunked-stream-payload!", 300) // ~6.6 KiB, > chunkSize*4
	lazy := &fakeLazyBlob{data: content, mediaType: "application/octet-stream"}
	resourceRepo := &fakeStreamingResourceRepo{blob: lazy}

	tr := &StreamLocalResource{
		Scheme:             streamTransformerScheme(t),
		RepoProvider:       &ociRepoProvider{repo: repo},
		ResourceRepository: resourceRepo,
	}

	spec := &v1alpha1.OCIStreamLocalResource{
		Type: v1alpha1.OCIStreamLocalResourceV1alpha1,
		ID:   "stream",
		Spec: &v1alpha1.OCIStreamLocalResourceSpec{
			Repository: ocispec.Repository{
				Type:    runtime.Type{Name: ocispec.Type, Version: "v1"},
				BaseUrl: address,
			},
			Component: "ocm.software/chunked-stream-test",
			Version:   "1.0.0",
			Resource:  wgetSourceResource("blob", nil), // nil digest => source digest unknown
		},
	}

	// Run the fused transformer. This performs the streaming chunked push.
	result, err := tr.Transform(ctx, spec)
	r.NoError(err)
	out, ok := result.(*v1alpha1.OCIStreamLocalResource)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotNil(out.Output.Resource)

	r.True(resourceRepo.streamCalled, "an idempotent, digest-unknown source must take the streaming path")
	r.False(resourceRepo.downloadCalled, "the materialized download must not be used")

	// The chunked upload protocol was used: a POST started the upload, one or more PATCH
	// requests streamed the chunks, and a PUT finalized it. A buffered monolithic push would
	// show zero PATCH requests.
	r.Positive(rec.count(http.MethodPost), "a chunked upload must POST to start the blob session")
	r.Greater(rec.count(http.MethodPatch), 1, "a multi-chunk streaming push must issue several PATCH requests")
	r.Positive(rec.count(http.MethodPut), "a chunked upload must PUT to finalize the blob")

	// The digest was computed on the fly during the stream and pinned on the LocalBlob.
	expectedDigest := godigest.FromString(content)
	localBlob := &v2.LocalBlob{}
	r.NoError(streamTransformerScheme(t).Convert(out.Output.Resource.Access, localBlob),
		"the transferred access must decode as a v2.LocalBlob")
	r.Equal(expectedDigest.String(), localBlob.LocalReference,
		"the LocalBlob reference must be the digest computed while streaming")

	// No temporary file was created during the transfer.
	assertNoTempFile(t, tempDir)

	// The content round-trips from the real registry with the computed digest.
	assertBlobRoundTrips(t, ctx, repo, spec, out, content, expectedDigest)
}

// Test_Integration_StreamLocalResource_RegularPush_DigestKnown covers the fused
// LocalBlob -> OCI registry path when the source digest IS known before the fetch.
// The content streams straight into the regular oras.Push (no OCI-layout tar, no
// temporary resource file), the known digest is carried through and pinned on the target
// LocalBlob, and the chunked streaming push (the digest-unknown path) is NOT used.
func Test_Integration_StreamLocalResource_RegularPush_DigestKnown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping regular-push transfer integration test in short mode")
	}
	r := require.New(t)
	ctx := t.Context()

	address := startStreamTargetRegistry(t)

	// Build a real *oci.Repository with chunked push ENABLED but a high threshold, so
	// a small blob is pushed monolithically. The recording client lets the test prove no
	// PATCH (chunk) request was issued: had the blob been streamed with an unknown digest,
	// PushStreaming would have chunked it. The small chunk size would split the blob into
	// several PATCH requests if it ever took the streaming path.
	const chunkSize = 256
	const chunkThreshold = 1 << 20 // 1 MiB; our blob is far below it, so Push stays monolithic.
	rec := &methodRecordingTransport{base: http.DefaultTransport.(*http.Transport).Clone(), methods: map[string]int{}}
	resolver, err := urlresolver.New(
		urlresolver.WithBaseURL(address),
		urlresolver.WithPlainHTTP(true),
		urlresolver.WithBaseClient(&http.Client{Transport: rec}),
		urlresolver.WithChunkedPush(chunkSize, chunkThreshold),
	)
	r.NoError(err)

	tempDir := t.TempDir()
	repo, err := oci.NewRepository(oci.WithResolver(resolver), oci.WithTempDir(tempDir))
	r.NoError(err)

	// A source whose digest and size are known up front (the digest-known case). Larger
	// than chunkSize so that, had it streamed, several PATCH requests would be observable.
	content := strings.Repeat("known-digest-stream-payload!", 60) // ~1.6 KiB, > chunkSize*4, < threshold
	src := &knownDigestSizeBlob{data: content, mediaType: "application/octet-stream"}
	resourceRepo := &fakeStreamingResourceRepo{blob: src}

	tr := &StreamLocalResource{
		Scheme:             streamTransformerScheme(t),
		RepoProvider:       &ociRepoProvider{repo: repo},
		ResourceRepository: resourceRepo,
	}

	spec := &v1alpha1.OCIStreamLocalResource{
		Type: v1alpha1.OCIStreamLocalResourceV1alpha1,
		ID:   "stream",
		Spec: &v1alpha1.OCIStreamLocalResourceSpec{
			Repository: ocispec.Repository{
				Type:    runtime.Type{Name: ocispec.Type, Version: "v1"},
				BaseUrl: address,
			},
			Component: "ocm.software/known-digest-stream-test",
			Version:   "1.0.0",
			Resource:  wgetSourceResource("blob", nil),
		},
	}

	// Run the fused transformer. This streams the blob into the regular push.
	result, err := tr.Transform(ctx, spec)
	r.NoError(err)
	out, ok := result.(*v1alpha1.OCIStreamLocalResource)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotNil(out.Output.Resource)

	r.True(resourceRepo.streamCalled, "an idempotent source must take the streaming path")
	r.False(resourceRepo.downloadCalled, "the materialized download must not be used")

	// The regular (non-streaming) push was used: the blob was uploaded monolithically
	// (POST then PUT) with NO PATCH, because pack skips the chunked streaming push when the
	// digest and size are already known.
	r.Positive(rec.count(http.MethodPost), "a blob upload must POST to start a session")
	r.Positive(rec.count(http.MethodPut), "a monolithic push must PUT the blob")
	r.Equal(0, rec.count(http.MethodPatch),
		"a digest-known push must NOT chunk: PushStreaming (the digest-unknown path) must not run")

	// The known digest was carried through to the target LocalBlob reference.
	expectedDigest := godigest.FromString(content)
	localBlob := &v2.LocalBlob{}
	r.NoError(streamTransformerScheme(t).Convert(out.Output.Resource.Access, localBlob),
		"the transferred access must decode as a v2.LocalBlob")
	r.Equal(expectedDigest.String(), localBlob.LocalReference,
		"the LocalBlob reference must be the known source digest")

	// No temporary file was created during the transfer.
	assertNoTempFile(t, tempDir)

	// The content round-trips from the real registry with the known digest.
	assertBlobRoundTrips(t, ctx, repo, spec, out, content, expectedDigest)
}

// assertNoTempFile asserts the configured temp dir stayed empty, proving the transfer
// streamed without materializing a temporary file.
func assertNoTempFile(t *testing.T, tempDir string) {
	t.Helper()
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Empty(t, entries, "a streaming push must not materialize a temporary file")
}

// assertBlobRoundTrips registers the transferred resource and reads it back from the
// registry, asserting the content and digest match what was streamed.
func assertBlobRoundTrips(t *testing.T, ctx context.Context, repo *oci.Repository,
	spec *v1alpha1.OCIStreamLocalResource, out *v1alpha1.OCIStreamLocalResource,
	content string, expectedDigest godigest.Digest,
) {
	t.Helper()
	r := require.New(t)

	convRes := descriptor.ConvertFromV2Resource(out.Output.Resource)
	r.NotNil(convRes)
	r.NoError(repo.AddComponentVersion(ctx, &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: spec.Spec.Component, Version: spec.Spec.Version},
			},
			Provider:  descriptor.Provider{Name: "test"},
			Resources: []descriptor.Resource{*convRes},
		},
	}))

	got, _, err := repo.GetLocalResource(ctx, spec.Spec.Component, spec.Spec.Version, convRes.ToIdentity())
	r.NoError(err)
	rc, err := got.ReadCloser()
	r.NoError(err)
	defer func() { _ = rc.Close() }()
	gotBytes, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, string(gotBytes), "the streamed blob must round-trip intact")
	r.Equal(expectedDigest.String(), godigest.FromBytes(gotBytes).String(),
		"the retrieved blob digest must match the expected digest")
}
