package cache

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry"

	identityv1 "ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
)

// ---- Options validation ----

func TestOptions_DefaultPolicyIsAlways(t *testing.T) {
	assert.Equal(t, RemotePolicyAlways, Defaults().RemotePolicy)
}

func TestOptions_ExplicitAlwaysAccepted(t *testing.T) {
	opts, err := Options{Dir: t.TempDir(), RemotePolicy: RemotePolicyAlways}.applyDefaults()
	require.NoError(t, err)
	assert.Equal(t, RemotePolicyAlways, opts.RemotePolicy)
}

func TestOptions_InvalidPolicyRejected(t *testing.T) {
	_, err := Options{Dir: t.TempDir(), RemotePolicy: "Never"}.applyDefaults()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported RemotePolicy")
}

// ---- BlobCache remote policy ----

func TestBlobCache_IfNotPresent_NoExistsOnHit(t *testing.T) {
	c := newTestCache(t, Options{RemotePolicy: RemotePolicyIfNotPresent})
	base := newFakeReadOnly()
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, []byte(`{"schemaVersion":2}`))

	rc1, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	_ = readAllAndClose(t, rc1)

	rc2, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	_ = readAllAndClose(t, rc2)

	assert.EqualValues(t, 1, base.fetches.Load(), "fetches")
	assert.EqualValues(t, 0, base.exists.Load(), "exists must not be called with IfNotPresent")
}

func TestBlobCache_Always_ExistsCalledOnHit(t *testing.T) {
	c := newTestCache(t, Options{RemotePolicy: RemotePolicyAlways})
	base := newFakeReadOnly()
	manifest := []byte(`{"schemaVersion":2}`)
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)

	rc1, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	assert.Equal(t, manifest, readAllAndClose(t, rc1))

	rc2, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	assert.Equal(t, manifest, readAllAndClose(t, rc2))

	assert.EqualValues(t, 1, base.fetches.Load(), "fetches")
	assert.EqualValues(t, 1, base.exists.Load(), "exists must be called on cache hit with Always")
}

func TestBlobCache_Always_ExistsDeniedReturnsError(t *testing.T) {
	c := newTestCache(t, Options{RemotePolicy: RemotePolicyAlways})
	base := newFakeReadOnly()
	manifest := []byte(`{"schemaVersion":2}`)
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)

	rc1, err := c.Fetch(t.Context(), base, desc)
	require.NoError(t, err)
	_ = readAllAndClose(t, rc1)

	base.remove(desc.Digest)

	_, err = c.Fetch(t.Context(), base, desc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote does not contain")
	assert.EqualValues(t, 1, base.exists.Load())
}

// ---- ReferenceCache remote policy ----

func TestReferenceCache_IfNotPresent_HitAvoidsUpstream(t *testing.T) {
	c := newTestRefCache(t, Options{RemotePolicy: RemotePolicyIfNotPresent})

	desc := ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest, Size: 1}
	ref, err := registry.ParseReference("ghcr.io/owner/repo@" + digest.FromString("payload").String())
	require.NoError(t, err)

	var resolves int
	up := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		resolves++
		return desc, nil
	})

	_, err = c.Resolve(t.Context(), up, ref)
	require.NoError(t, err)

	_, err = c.Resolve(t.Context(), up, ref)
	require.NoError(t, err)

	assert.Equal(t, 1, resolves, "IfNotPresent must not call upstream on a digest hit")
}

// TestReferenceCache_IfNotPresent_TagAlwaysResolvesUpstream pins that
// the IfNotPresent shortcut governs authorisation, not freshness: a tag
// is mutable, so it is resolved upstream even when a mapping is cached.
func TestReferenceCache_IfNotPresent_TagAlwaysResolvesUpstream(t *testing.T) {
	c := newTestRefCache(t, Options{RemotePolicy: RemotePolicyIfNotPresent})

	desc := ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest, Size: 1}
	ref, err := registry.ParseReference("ghcr.io/owner/repo:v1")
	require.NoError(t, err)

	var resolves int
	up := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		resolves++
		return desc, nil
	})

	_, err = c.Resolve(t.Context(), up, ref)
	require.NoError(t, err)

	_, err = c.Resolve(t.Context(), up, ref)
	require.NoError(t, err)

	assert.Equal(t, 2, resolves, "a tag must be resolved upstream on every call")
}

func TestReferenceCache_Always_AlwaysCallsUpstream(t *testing.T) {
	c := newTestRefCache(t, Options{RemotePolicy: RemotePolicyAlways})

	desc := ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest, Size: 1}
	ref, err := registry.ParseReference("ghcr.io/owner/repo:v1")
	require.NoError(t, err)

	var resolves atomic.Int64
	up := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		resolves.Add(1)
		return desc, nil
	})

	_, err = c.Resolve(t.Context(), up, ref)
	require.NoError(t, err)

	_, err = c.Resolve(t.Context(), up, ref)
	require.NoError(t, err)

	assert.EqualValues(t, 2, resolves.Load(), "Always must call upstream on every resolve")
}

// ---- Provider scope ----

func TestProviderScope_SameIdentityProducesSameKey(t *testing.T) {
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}
	assert.Equal(t, RepositoryKey(id), RepositoryKey(id))
}

func TestProviderScope_DifferentRepositoryPathDifferentKey(t *testing.T) {
	id1 := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}
	id2 := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/other"}
	assert.NotEqual(t, RepositoryKey(id1), RepositoryKey(id2))
}

func TestProviderScope_ReferenceCacheDirDeterministic(t *testing.T) {
	base := filepath.Join(t.TempDir(), "refcache")
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}
	scope := RepositoryKey(id)
	assert.Equal(t, filepath.Join(base, scope), filepath.Join(base, RepositoryKey(id)))
}
