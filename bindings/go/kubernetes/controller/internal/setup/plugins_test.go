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

		bc, err := setup.NewBlobCache(t.TempDir(), &ocicachev1alpha1.Config{MaxBlobSize: new(int64(16))})
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

		bc, err := setup.NewBlobCache(t.TempDir(), &ocicachev1alpha1.Config{MaxTotalSize: new(int64(17))})
		r.NoError(err)

		fetchAll(t, bc, repo, first, second)
		r.False(bc.Has(first.Digest), "the older blob must be evicted to fit maxTotalSize")
		r.True(bc.Has(second.Digest))
	})

	t.Run("maxTotalSize 0 lifts the default limit", func(t *testing.T) {
		r := require.New(t)
		repo := memoryRepository{memory.New()}
		desc := pushDescriptor(t, repo, 16)

		bc, err := setup.NewBlobCache(t.TempDir(), &ocicachev1alpha1.Config{MaxTotalSize: new(int64(0))})
		r.NoError(err)

		fetchAll(t, bc, repo, desc)
		r.True(bc.Has(desc.Digest))
	})

	t.Run("ttl expires cached blobs", func(t *testing.T) {
		r := require.New(t)
		repo := memoryRepository{memory.New()}
		desc := pushDescriptor(t, repo, 16)

		bc, err := setup.NewBlobCache(t.TempDir(), &ocicachev1alpha1.Config{TTL: typesv1.NewDuration(50 * time.Millisecond)})
		r.NoError(err)

		rc, err := bc.Fetch(t.Context(), repo, desc)
		r.NoError(err)
		r.NoError(rc.Close())
		r.True(bc.Has(desc.Digest))
		r.Eventually(func() bool { return !bc.Has(desc.Digest) }, 5*time.Second, 10*time.Millisecond,
			"a blob older than ttl must expire")
	})
}

func TestNewBlobCache_Disabled(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()
	bc, err := setup.NewBlobCache(dir, &ocicachev1alpha1.Config{Enabled: new(false)})
	r.NoError(err)
	r.Nil(bc)
	r.NoDirExists(filepath.Join(dir, "ocm-oci-cas"))
}
