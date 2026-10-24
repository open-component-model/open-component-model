package cache

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

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

func TestBlobCache_Always_SharedFlightChecksEachCaller(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{RemotePolicy: RemotePolicyAlways})
	base := newFakeReadOnly()
	manifest := []byte(`{"schemaVersion":2}`)
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)
	allowed := newGated(base, 1)
	denied := newFakeReadOnly()

	allowedErr := make(chan error, 1)
	go func() {
		rc, err := c.Fetch(t.Context(), allowed, desc)
		if err == nil {
			_ = readAllAndClose(t, rc)
		}
		allowedErr <- err
	}()
	allowed.waitBlocked(t, 1)

	deniedErr := make(chan error, 1)
	go func() {
		rc, err := c.Fetch(t.Context(), denied, desc)
		if err == nil {
			_ = rc.Close()
		}
		deniedErr <- err
	}()
	// singleflight exposes no join signal. A late joiner hits the disk cache and is denied there,
	// so the sleep can only weaken the regression check, never flake the test.
	time.Sleep(100 * time.Millisecond)
	close(allowed.release)

	r.NoError(<-allowedErr)
	err := <-deniedErr
	r.Error(err)
	r.Contains(err.Error(), "remote does not contain or allow access")
	r.EqualValues(1, base.fetches.Load(), "the denied caller must not trigger a second upstream fetch")
	r.EqualValues(1, denied.exists.Load(), "the denied caller must check its own upstream")
}

// ---- ReferenceCache remote policy ----

func TestReferenceCache_IfNotPresent_HitAvoidsUpstream(t *testing.T) {
	c := newTestRefCache(t, Options{RemotePolicy: RemotePolicyIfNotPresent})

	desc := ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest, Size: 1}
	ref, err := registry.ParseReference("ghcr.io/owner/repo@" + digest.FromString("payload").String())
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

	assert.EqualValues(t, 1, resolves.Load(), "IfNotPresent must not call upstream on a digest hit")
}

// TestReferenceCache_IfNotPresent_TagAlwaysResolvesUpstream pins that
// the IfNotPresent shortcut governs authorisation, not freshness: a tag
// is mutable, so it is resolved upstream even when a mapping is cached.
func TestReferenceCache_IfNotPresent_TagAlwaysResolvesUpstream(t *testing.T) {
	c := newTestRefCache(t, Options{RemotePolicy: RemotePolicyIfNotPresent})

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

	assert.EqualValues(t, 2, resolves.Load(), "a tag must be resolved upstream on every call")
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

func TestReferenceCache_Always_ConcurrentCallersResolveIndependently(t *testing.T) {
	r := require.New(t)
	c := newTestRefCache(t, Options{RemotePolicy: RemotePolicyAlways})

	desc := ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest, Size: 1}
	ref, err := registry.ParseReference("ghcr.io/owner/repo:v1")
	r.NoError(err)

	started := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	allowed := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		close(started)
		<-release
		return desc, nil
	})
	denied := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		return ociImageSpecV1.Descriptor{}, errors.New("access denied")
	})

	go func() { _, _ = c.Resolve(t.Context(), allowed, ref) }()
	<-started

	deniedErr := make(chan error, 1)
	go func() {
		_, err := c.Resolve(t.Context(), denied, ref)
		deniedErr <- err
	}()

	select {
	case err := <-deniedErr:
		r.ErrorContains(err, "access denied")
	case <-time.After(5 * time.Second):
		t.Fatal("denied caller waited on another caller's in-flight resolve")
	}
}

// ---- Provider scope ----

func TestProviderScope_SameIdentityProducesSameKey(t *testing.T) {
	id := &identityv1.OCIRegistryIdentity{Hostname: "ghcr.io", Path: "owner/repo"}
	first := RepositoryKey(id)
	second := RepositoryKey(id)
	assert.Equal(t, first, second)
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
