package integration_test

import (
	"bytes"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/cache"
)

// patchUploads returns how many PATCH requests the registry observed against a
// blob-upload session. A chunked upload issues PATCH chunks; a monolithic one
// does not, so a positive count proves the blob was uploaded in chunks.
func (c *registryCalls) patchUploads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count["PATCH uploads"]
}

// Test_Integration_OCICache_ChunkingSurvivesCache guards the stock-CLI path,
// which always configures a blob and reference cache. Before the fix the cache
// decorator hid the chunked store, so streaming pushes fell back to a monolithic
// PUT. It wires the provider like the CLI and asserts that a blob with unknown
// size and digest (forcing PushStreaming) still uploads with PATCH chunks.
func Test_Integration_OCICache_ChunkingSurvivesCache(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping cache chunked-push integration test in short mode")
	}
	t.Parallel()
	ctx := t.Context()
	r := require.New(t)

	baseURL, calls := startProxiedRegistry(t, ctx)

	// CLI-faithful wiring: a blob and reference cache both configured, exactly
	// as cli/internal/plugin/builtin/oci/register.go sets them up. Chunked push
	// is enabled by default inside NewResolver, so no extra opt-in is needed.
	prov := cachingProvider(t.TempDir(), cache.RemotePolicyIfNotPresent)
	repo := repoFor(t, ctx, prov, baseURL)

	const (
		component = "ocm.software/cache-chunk-test"
		version   = "v1.0.0"
	)

	// A payload comfortably above the default chunk size (4 MiB) so chunking
	// spans more than one chunk.
	payload := deterministicPayload(20<<20, "cache-chunk-payload-")
	wantDigest := digest.FromBytes(payload)

	resource := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "chunked", Version: version}},
		Type:        "blob",
		Relation:    descriptor.LocalRelation,
		Access:      &v2.LocalBlob{MediaType: "application/octet-stream"},
	}

	calls.reset()
	newRes, err := repo.AddLocalResource(ctx, component, version, resource,
		&streamOnlyBlob{reader: bytes.NewReader(payload), mediaType: "application/octet-stream"})
	r.NoError(err)
	r.NotNil(newRes)

	t.Logf("after cached large-blob add: %d PATCH uploads (%v)", calls.patchUploads(), calls.dump())
	r.Positive(calls.patchUploads(),
		"a 20 MiB blob with unknown size/digest pushed through the cached resolver must upload in PATCH chunks, not a monolithic PUT (chunking must survive the cache)")

	// The component version round-trips and the resource digest matches.
	cd := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			Provider:      descriptor.Provider{Name: "ocm.software/test"},
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version}},
			Resources:     []descriptor.Resource{*newRes},
		},
	}
	r.NoError(repo.AddComponentVersion(ctx, cd))

	blobRC, gotRes, err := repo.GetLocalResource(ctx, component, version, resource.ToIdentity())
	r.NoError(err)
	r.NotNil(gotRes)
	rc, err := blobRC.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { _ = rc.Close() })
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(rc)
	r.NoError(err)
	r.Equal(wantDigest, digest.FromBytes(buf.Bytes()))
}
