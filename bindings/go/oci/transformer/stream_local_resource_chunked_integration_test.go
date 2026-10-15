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

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// methodRecordingTransport counts the HTTP methods issued through it so a test
// can prove the OCI chunked blob upload protocol (POST to start, PATCH per chunk,
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

// Test_Integration_StreamLocalResource_ChunkedPush_DigestUnknown  tests the fused
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

	// 1. Start a real OCI registry (anonymous push, validation off like the chunked push test).
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	registryContainer, err := registry.Run(runCtx, "registry:3.0.0",
		testcontainers.WithEnv(map[string]string{"REGISTRY_VALIDATION_DISABLED": "true"}),
	)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(registryContainer)) })
	address, err := registryContainer.HostAddress(ctx)
	r.NoError(err)

	// 2. Build a real *oci.Repository whose client records HTTP methods, with chunked push
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

	// 3. A size-unknown, idempotent lazy source whose descriptor carries NO digest: the
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

	// 4. Run the fused transformer. This performs the streaming chunked push.
	result, err := tr.Transform(ctx, spec)
	r.NoError(err)
	out, ok := result.(*v1alpha1.OCIStreamLocalResource)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotNil(out.Output.Resource)

	r.True(resourceRepo.streamCalled, "an idempotent, digest-unknown source must take the streaming path")
	r.False(resourceRepo.downloadCalled, "the materialized download must not be used")

	// 5. The chunked upload protocol was used: a POST started the upload, one or more PATCH
	// requests streamed the chunks, and a PUT finalized it. A buffered monolithic push would
	// show zero PATCH requests.
	r.Positive(rec.count(http.MethodPost), "a chunked upload must POST to start the blob session")
	r.Greater(rec.count(http.MethodPatch), 1, "a multi-chunk streaming push must issue several PATCH requests")
	r.Positive(rec.count(http.MethodPut), "a chunked upload must PUT to finalize the blob")

	// 6. The digest was computed on the fly during the stream and pinned on the LocalBlob.
	expectedDigest := godigest.FromString(content)
	localBlob := &v2.LocalBlob{}
	r.NoError(streamTransformerScheme(t).Convert(out.Output.Resource.Access, localBlob),
		"the transferred access must decode as a v2.LocalBlob")
	r.Equal(expectedDigest.String(), localBlob.LocalReference,
		"the LocalBlob reference must be the digest computed while streaming")

	// 7. No temporary file was created during the transfer.
	entries, err := os.ReadDir(tempDir)
	r.NoError(err)
	r.Empty(entries, "a streaming chunked push must not materialize a temporary file")

	// 8. The content round-trips from the real registry with the computed digest.
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
		"the retrieved blob digest must match the digest computed during streaming")
}
