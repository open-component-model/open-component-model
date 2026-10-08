package integration_test

import (
	"bytes"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
)

// patchUploads returns how many PATCH requests the registry observed against a
// blob-upload session. A chunked upload issues one POST, one or more PATCH
// chunks, then a PUT; a monolithic upload issues POST+PUT with no PATCH. So a
// positive count proves the blob was uploaded in chunks.
func (c *registryCalls) patchUploads() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.count["PATCH uploads"]
}

// Test_Integration_OCICache_ChunkingSurvivesCache is the regression guard for
// the stock-CLI path: the CLI always configures a blob and reference cache
// (cli/internal/plugin/builtin/oci/register.go). Before the fix the cache
// decorator wrapped the raw oras *remote.Repository, hiding the chunked store,
// so large-blob pushes silently fell back to a monolithic PUT. This test wires
// the provider exactly like the CLI (blob + reference cache) and asserts that a
// blob larger than DefaultChunkThreshold is still uploaded with PATCH chunk
// requests, i.e. chunking survived the cache.
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

	// A payload comfortably above DefaultChunkThreshold (16 MiB) so chunking
	// must engage and span more than one chunk (16 MiB default chunk size).
	r.EqualValues(16<<20, remotestore.DefaultChunkThreshold)
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
		inmemory.New(bytes.NewReader(payload)))
	r.NoError(err)
	r.NotNil(newRes)

	t.Logf("after cached large-blob add: %d PATCH uploads (%v)", calls.patchUploads(), calls.dump())
	r.Positive(calls.patchUploads(),
		"a >16 MiB blob pushed through the cached resolver must upload in PATCH chunks, not a monolithic PUT (chunking must survive the cache)")

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

	blobRC, gotRes, err := repo.GetLocalResource(ctx, component, version, resource.ElementMeta.ToIdentity())
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
