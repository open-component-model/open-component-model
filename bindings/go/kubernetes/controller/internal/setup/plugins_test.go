package setup_test

import (
	"bytes"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content/memory"
	"github.com/oras-project/oras-go/v3/registry/remote/properties"
	"github.com/stretchr/testify/require"

	typesv1 "ocm.software/open-component-model/bindings/go/configuration/types/v1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	ocicache "ocm.software/open-component-model/bindings/go/oci/cache"
	ocicachev1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/config/cache/v1alpha1"
	componentConfig "ocm.software/open-component-model/bindings/go/oci/spec/config/component"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
)

// memoryRepository is an in-memory upstream for the blob cache.
type memoryRepository struct {
	*memory.Store
}

func (memoryRepository) ParseReference(reference string) (properties.Reference, error) {
	ref := properties.Reference{Registry: "registry.example", Repository: "repo"}
	if _, err := digest.Parse(reference); err == nil {
		ref.Digest = reference
	} else {
		ref.Tag = reference
	}
	return ref, nil
}

// pushDescriptor pushes a blob with a media type the blob cache admits.
func pushDescriptor(t *testing.T, repo memoryRepository, size int) ociImageSpecV1.Descriptor {
	t.Helper()
	data := bytes.Repeat([]byte("a"), size)
	desc := ociImageSpecV1.Descriptor{
		MediaType: ocidescriptor.MediaTypeComponentDescriptorV2,
		Digest:    digest.FromBytes(data),
		Size:      int64(size),
	}
	require.NoError(t, repo.Push(t.Context(), desc, bytes.NewReader(data)))
	return desc
}

// fetchAll fetches and reads every desc through bc.
func fetchAll(t *testing.T, bc *ocicache.BlobCache, repo memoryRepository, descs ...ociImageSpecV1.Descriptor) {
	t.Helper()
	for _, desc := range descs {
		rc, err := bc.Fetch(t.Context(), repo, desc)
		require.NoError(t, err)
		_, err = io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
	}
}

func TestNewBlobCache(t *testing.T) {
	t.Run("maxBlobSize caps the blobs the cache stores", func(t *testing.T) {
		r := require.New(t)
		repo := memoryRepository{memory.New()}
		small := pushDescriptor(t, repo, 16)
		large := pushDescriptor(t, repo, 17)

		bc, err := setup.NewBlobCache(t.TempDir(), &ocicachev1alpha1.Config{Blob: &ocicachev1alpha1.BlobCacheConfig{MaxBlobSize: new(int64(16))}}, 0)
		r.NoError(err)

		for _, desc := range []ociImageSpecV1.Descriptor{small, large} {
			rc, err := bc.Fetch(t.Context(), repo, desc)
			r.NoError(err)
			_, err = io.ReadAll(rc)
			r.NoError(err)
			r.NoError(rc.Close())
		}
		r.True(bc.Has(small.Digest))
		r.False(bc.Has(large.Digest), "a blob above maxBlobSize must not be cached")
	})

	t.Run("maxTotalSize evicts the least recently used blob", func(t *testing.T) {
		r := require.New(t)
		repo := memoryRepository{memory.New()}
		first, second := pushDescriptor(t, repo, 16), pushDescriptor(t, repo, 17)

		bc, err := setup.NewBlobCache(t.TempDir(), &ocicachev1alpha1.Config{Blob: &ocicachev1alpha1.BlobCacheConfig{MaxTotalSize: new(int64(17))}}, 0)
		r.NoError(err)

		fetchAll(t, bc, repo, first, second)
		r.False(bc.Has(first.Digest), "the older blob must be evicted to fit maxTotalSize")
		r.True(bc.Has(second.Digest))
	})

	t.Run("maxTotalSize 0 lifts the default limit", func(t *testing.T) {
		r := require.New(t)
		repo := memoryRepository{memory.New()}
		desc := pushDescriptor(t, repo, 16)

		bc, err := setup.NewBlobCache(t.TempDir(), &ocicachev1alpha1.Config{Blob: &ocicachev1alpha1.BlobCacheConfig{MaxTotalSize: new(int64(0))}}, 0)
		r.NoError(err)

		fetchAll(t, bc, repo, desc)
		r.True(bc.Has(desc.Digest))
	})

	t.Run("ttl expires cached blobs", func(t *testing.T) {
		r := require.New(t)
		repo := memoryRepository{memory.New()}
		desc := pushDescriptor(t, repo, 16)

		bc, err := setup.NewBlobCache(t.TempDir(), &ocicachev1alpha1.Config{Blob: &ocicachev1alpha1.BlobCacheConfig{TTL: typesv1.NewDuration(50 * time.Millisecond)}}, 0)
		r.NoError(err)

		rc, err := bc.Fetch(t.Context(), repo, desc)
		r.NoError(err)
		r.NoError(rc.Close())
		r.True(bc.Has(desc.Digest))
		r.Eventually(func() bool { return !bc.Has(desc.Digest) }, 5*time.Second, 10*time.Millisecond,
			"a blob older than ttl must expire")
	})
}

func TestNewReferenceCache(t *testing.T) {
	newCache := func(cfg *ocicachev1alpha1.Config) (*ocicache.ReferenceCache, string) {
		t.Helper()
		dir := t.TempDir()
		rc, err := setup.NewReferenceCache(dir, cfg)
		require.NoError(t, err)
		return rc, filepath.Join(dir, "ocm-oci-refcache")
	}
	on := &ocicachev1alpha1.ReferenceCacheConfig{Enabled: new(true)}

	t.Run("off by default", func(t *testing.T) {
		rc, dir := newCache(nil)
		require.Nil(t, rc)
		require.NoDirExists(t, dir)
		rc, _ = newCache(&ocicachev1alpha1.Config{Blob: &ocicachev1alpha1.BlobCacheConfig{Enabled: new(true)}})
		require.Nil(t, rc)
	})
	t.Run("on if enabled", func(t *testing.T) {
		rc, dir := newCache(&ocicachev1alpha1.Config{Reference: on})
		require.NotNil(t, rc)
		require.DirExists(t, dir)
	})
	t.Run("maxEntries limits the tags of all repositories together", func(t *testing.T) {
		rc, _ := newCache(&ocicachev1alpha1.Config{Reference: &ocicachev1alpha1.ReferenceCacheConfig{Enabled: new(true), MaxEntries: new(1)}})
		require.NotNil(t, rc)
		a := properties.Reference{Registry: "registry.example", Repository: "a", Tag: "v1"}
		b := properties.Reference{Registry: "registry.example", Repository: "b", Tag: "v1"}
		rc.Add(a, ociImageSpecV1.Descriptor{Digest: digest.FromString("a")})
		rc.Add(b, ociImageSpecV1.Descriptor{Digest: digest.FromString("b")})
		_, ok := rc.Lookup(a)
		require.False(t, ok, "the tag of repository a must make room for the tag of repository b")
		_, ok = rc.Lookup(b)
		require.True(t, ok)
	})
	t.Run("off under Always", func(t *testing.T) {
		rc, _ := newCache(&ocicachev1alpha1.Config{RemotePolicy: ocicachev1alpha1.RemotePolicyAlways, Reference: on})
		require.Nil(t, rc)
	})
}

// admits reports whether bc caches desc, the checks [ocicache.BlobCache.Fetch] makes. The total size
// limit is left out: it is far above every size here.
func admits(bc *ocicache.BlobCache, desc ociImageSpecV1.Descriptor) bool {
	return bc.Accept(desc) && (bc.MaxBlobSize() == 0 || desc.Size <= bc.MaxBlobSize())
}

func blob(mediaType string, size int64) ociImageSpecV1.Descriptor {
	return ociImageSpecV1.Descriptor{MediaType: mediaType, Size: size, Digest: digest.FromString(mediaType)}
}

func TestNewBlobCache_Disabled(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()
	bc, err := setup.NewBlobCache(dir, &ocicachev1alpha1.Config{Blob: &ocicachev1alpha1.BlobCacheConfig{Enabled: new(false)}}, 0)
	r.NoError(err)
	r.Nil(bc)
	r.NoDirExists(filepath.Join(dir, "ocm-oci-cas"))
}

func TestNewBlobCache_Admission(t *testing.T) {
	const layerCap = 2 << 20
	const layer = ociImageSpecV1.MediaTypeImageLayer
	tests := []struct {
		name     string
		layerCap int64
		desc     ociImageSpecV1.Descriptor
		want     bool
	}{
		{name: "manifest", layerCap: layerCap, desc: blob(ociImageSpecV1.MediaTypeImageManifest, 4<<20), want: true},
		{name: "component config", layerCap: layerCap, desc: blob(componentConfig.MediaType, 512), want: true},
		{name: "component descriptor", layerCap: layerCap, desc: blob(ocidescriptor.MediaTypeComponentDescriptorYAML, 4<<20), want: true},
		{name: "descriptor at the descriptor limit", layerCap: layerCap, desc: blob(ocidescriptor.MediaTypeComponentDescriptorYAML, ocicache.DefaultMaxBlobSize), want: true},
		{name: "layer at the cap", layerCap: layerCap, desc: blob(layer, layerCap), want: true},
		{name: "layer above the cap", layerCap: layerCap, desc: blob(layer, layerCap+1), want: false},
		{name: "layer of unknown size", layerCap: layerCap, desc: blob(layer, 0), want: false},
		{name: "no download limit: layer at the default blob limit", layerCap: 0, desc: blob(layer, ocicache.DefaultMaxBlobSize), want: true},
		{name: "no download limit: layer above the default blob limit", layerCap: 0, desc: blob(layer, ocicache.DefaultMaxBlobSize+1), want: false},
		{name: "cap above the default blob limit", layerCap: 2 * ocicache.DefaultMaxBlobSize, desc: blob(layer, 2*ocicache.DefaultMaxBlobSize), want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			bc, err := setup.NewBlobCache(t.TempDir(), nil, tc.layerCap)
			r.NoError(err)
			r.Equal(tc.want, admits(bc, tc.desc))
		})
	}
}

func TestNewBlobCache_MaxBlobSizeCapsLayers(t *testing.T) {
	r := require.New(t)
	bc, err := setup.NewBlobCache(t.TempDir(), &ocicachev1alpha1.Config{Blob: &ocicachev1alpha1.BlobCacheConfig{MaxBlobSize: new(int64(1 << 10))}}, 2<<20)
	r.NoError(err)
	r.True(admits(bc, blob(ociImageSpecV1.MediaTypeImageLayer, 1<<10)))
	r.False(admits(bc, blob(ociImageSpecV1.MediaTypeImageLayer, 1<<10+1)), "maxBlobSize must cap layers below the download limit")
}

func TestNewBlobCache_NegativeLayerCapIsRejected(t *testing.T) {
	r := require.New(t)
	_, err := setup.NewBlobCache(t.TempDir(), nil, -1)
	r.Error(err)
}
