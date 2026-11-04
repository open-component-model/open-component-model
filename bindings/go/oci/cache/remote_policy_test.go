package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
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

func TestOptions_DefaultPolicyIsIfNotPresent(t *testing.T) {
	assert.Equal(t, RemotePolicyIfNotPresent, Defaults().RemotePolicy)
}

func TestOptions_DefaultLimitsPerCache(t *testing.T) {
	r := require.New(t)

	blobs, err := NewBlobCache(Options{Dir: t.TempDir()})
	r.NoError(err)
	r.Equal(DefaultBlobMaxEntries, blobs.opts.MaxEntries)
	r.EqualValues(DefaultMaxBlobSize, blobs.MaxBlobSize())

	refs, err := NewReferenceCache(Options{Dir: t.TempDir()})
	r.NoError(err)
	r.Equal(DefaultReferenceMaxEntries, refs.opts.MaxEntries)
}

func TestOptions_ExplicitAlwaysAccepted(t *testing.T) {
	opts, err := Options{Dir: t.TempDir(), RemotePolicy: RemotePolicyAlways}.applyDefaults(DefaultBlobMaxEntries)
	require.NoError(t, err)
	assert.Equal(t, RemotePolicyAlways, opts.RemotePolicy)
}

func TestOptions_InvalidPolicyRejected(t *testing.T) {
	_, err := Options{Dir: t.TempDir(), RemotePolicy: "Never"}.applyDefaults(DefaultBlobMaxEntries)
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
	assert.EqualValues(t, 1, base.exists.Load(), "the miss proves access through its own fetch, only the hit calls exists")
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

func TestBlobCache_Always_MissesAreNotCollapsed(t *testing.T) {
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
			assert.Equal(t, manifest, readAllAndClose(t, rc))
		}
		allowedErr <- err
	}()
	allowed.waitBlocked(t, 1)

	// The denied caller fails on its own fetch while the allowed caller's fetch is still in flight.
	_, err := c.Fetch(t.Context(), denied, desc)
	r.Error(err)
	r.EqualValues(1, denied.fetches.Load(), "the denied caller must fetch with its own upstream")
	r.Zero(denied.exists.Load())

	close(allowed.release)
	r.NoError(<-allowedErr)
	r.EqualValues(1, base.fetches.Load())
	r.Zero(base.exists.Load(), "a miss proves access through its own fetch")
}

// lyingRepository claims to hold every blob, but can only serve its own bytes.
type lyingRepository struct {
	*fakeReadOnly
	served []byte
}

func (s *lyingRepository) Exists(context.Context, ociImageSpecV1.Descriptor) (bool, error) {
	s.exists.Add(1)
	return true, nil
}

func (s *lyingRepository) Fetch(context.Context, ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	s.fetches.Add(1)
	return io.NopCloser(bytes.NewReader(s.served)), nil
}

func TestBlobCache_Always_LyingRepositoryDoesNotGetAnotherRepositorysBlob(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{RemotePolicy: RemotePolicyAlways})
	victim := newFakeRepository("registry.example", "victim")
	manifest := []byte(`{"schemaVersion":2,"private":true}`)
	desc := victim.put(ociImageSpecV1.MediaTypeImageManifest, manifest)

	rc, err := c.Fetch(t.Context(), victim, desc)
	r.NoError(err)
	r.Equal(manifest, readAllAndClose(t, rc))

	attacker := &lyingRepository{
		fakeReadOnly: newFakeRepository("evil.example", "attacker"),
		served:       []byte(`{"schemaVersion":2,"private":false}`),
	}
	_, err = c.Fetch(t.Context(), attacker, desc)
	r.Error(err, "the attacker's Exists must not unlock the cached blob")
	r.EqualValues(1, attacker.fetches.Load(), "the attacker must have to serve the blob itself")
}

func TestBlobCache_Always_RepositoryThatServesTheBlobGetsHits(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{RemotePolicy: RemotePolicyAlways})
	first := newFakeRepository("registry.example", "first")
	second := newFakeRepository("registry.example", "second")
	manifest := []byte(`{"schemaVersion":2}`)
	desc := first.put(ociImageSpecV1.MediaTypeImageManifest, manifest)
	second.put(ociImageSpecV1.MediaTypeImageManifest, manifest)

	rc, err := c.Fetch(t.Context(), first, desc)
	r.NoError(err)
	_ = readAllAndClose(t, rc)

	rc, err = c.Fetch(t.Context(), second, desc)
	r.NoError(err)
	r.Equal(manifest, readAllAndClose(t, rc))
	r.EqualValues(1, second.fetches.Load(), "the first read from a repository must fetch")
	r.Zero(second.exists.Load())

	rc, err = c.Fetch(t.Context(), second, desc)
	r.NoError(err)
	r.Equal(manifest, readAllAndClose(t, rc))
	r.EqualValues(1, second.fetches.Load(), "the repository served the blob, so this is a hit")
	r.EqualValues(1, second.exists.Load())
}

func TestBlobCache_Always_WhatARepositoryServedSurvivesARestart(t *testing.T) {
	r := require.New(t)
	dir := filepath.Join(t.TempDir(), "blobcache")
	base := newFakeReadOnly()
	manifest := []byte(`{"schemaVersion":2}`)
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, manifest)

	rc, err := newTestCache(t, Options{Dir: dir, RemotePolicy: RemotePolicyAlways}).Fetch(t.Context(), base, desc)
	r.NoError(err)
	_ = readAllAndClose(t, rc)

	restarted := newTestCache(t, Options{Dir: dir, RemotePolicy: RemotePolicyAlways})
	rc, err = restarted.Fetch(t.Context(), base, desc)
	r.NoError(err)
	r.Equal(manifest, readAllAndClose(t, rc))
	r.EqualValues(1, base.fetches.Load(), "the repository served the blob before the restart")
	r.EqualValues(1, base.exists.Load())

	attacker := &lyingRepository{
		fakeReadOnly: newFakeRepository("evil.example", "attacker"),
		served:       []byte(`{"schemaVersion":3}`),
	}
	_, err = restarted.Fetch(t.Context(), attacker, desc)
	r.Error(err, "a restart must not unlock the blob for another repository")
	r.EqualValues(1, attacker.fetches.Load())
}

// A reseeded blob gets a fresh TTL, so what a repository served must not expire
// before the blob does.
func TestBlobCache_Always_WhatARepositoryServedDoesNotExpireBeforeTheBlob(t *testing.T) {
	r := require.New(t)
	const ttl = 200 * time.Millisecond
	dir := filepath.Join(t.TempDir(), "blobcache")
	base := newFakeReadOnly()
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, []byte(`{"schemaVersion":2}`))

	// A long TTL here, as a running cache would delete the blob once it expires.
	rc, err := newTestCache(t, Options{Dir: dir, TTL: time.Hour, RemotePolicy: RemotePolicyAlways}).Fetch(t.Context(), base, desc)
	r.NoError(err)
	_ = readAllAndClose(t, rc)

	time.Sleep(ttl + 100*time.Millisecond)

	restarted := newTestCache(t, Options{Dir: dir, TTL: ttl, RemotePolicy: RemotePolicyAlways})
	r.True(restarted.Has(desc.Digest), "precondition: the blob must have survived the restart")
	rc, err = restarted.Fetch(t.Context(), base, desc)
	r.NoError(err)
	_ = readAllAndClose(t, rc)
	r.EqualValues(1, base.fetches.Load(), "the blob and what its repository served must age together")
	r.EqualValues(1, base.exists.Load())
}

// unidentifiableRepository cannot tell which repository it belongs to.
type unidentifiableRepository struct{ *fakeReadOnly }

func (*unidentifiableRepository) ParseReference(string) (registry.Reference, error) {
	return registry.Reference{}, errors.New("no repository")
}

func TestBlobCache_Always_UnidentifiableRepositoryIsRejected(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{RemotePolicy: RemotePolicyAlways})
	base := &unidentifiableRepository{newFakeReadOnly()}
	desc := base.put(ociImageSpecV1.MediaTypeImageManifest, []byte(`{"schemaVersion":2}`))

	_, err := c.Fetch(t.Context(), base, desc)
	r.ErrorContains(err, "identify repository")
	r.Zero(base.fetches.Load())
}

// TestBlobCache_IfNotPresent_HitIsNotBoundToRepository pins the accepted risk of
// [RemotePolicyIfNotPresent]: whoever knows a digest gets a cached blob.
func TestBlobCache_IfNotPresent_HitIsNotBoundToRepository(t *testing.T) {
	r := require.New(t)
	c := newTestCache(t, Options{RemotePolicy: RemotePolicyIfNotPresent})
	first := newFakeRepository("registry.example", "first")
	manifest := []byte(`{"schemaVersion":2}`)
	desc := first.put(ociImageSpecV1.MediaTypeImageManifest, manifest)

	rc, err := c.Fetch(t.Context(), first, desc)
	r.NoError(err)
	_ = readAllAndClose(t, rc)

	other := newFakeRepository("registry.example", "other")
	rc, err = c.Fetch(t.Context(), other, desc)
	r.NoError(err)
	r.Equal(manifest, readAllAndClose(t, rc))
	r.Zero(other.fetches.Load())
	r.Zero(other.exists.Load())
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
	allowed := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		close(started)
		<-release
		return desc, nil
	})
	denied := resolverFn(func(_ context.Context, _ string) (ociImageSpecV1.Descriptor, error) {
		return ociImageSpecV1.Descriptor{}, errors.New("access denied")
	})

	allowedErr := make(chan error, 1)
	go func() {
		_, err := c.Resolve(t.Context(), allowed, ref)
		allowedErr <- err
	}()
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

	// Join the allowed caller: its Add writes the snapshot into the TempDir,
	// which must finish before the TempDir cleanup removes the directory.
	close(release)
	r.NoError(<-allowedErr)
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
