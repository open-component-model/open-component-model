package setup_test

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/oras-project/oras-go/v3/content/memory"
	"github.com/oras-project/oras-go/v3/registry/remote/properties"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
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

	t.Run("ttl expires cached blobs", func(t *testing.T) {
		r := require.New(t)
		repo := memoryRepository{memory.New()}
		desc := pushDescriptor(t, repo, 16)

		bc, err := setup.NewBlobCache(t.TempDir(), &ocicachev1alpha1.Config{TTL: ocicachev1alpha1.NewDuration(50 * time.Millisecond)})
		r.NoError(err)

		rc, err := bc.Fetch(t.Context(), repo, desc)
		r.NoError(err)
		r.NoError(rc.Close())
		r.True(bc.Has(desc.Digest))
		r.Eventually(func() bool { return !bc.Has(desc.Digest) }, 5*time.Second, 10*time.Millisecond,
			"a blob older than ttl must expire")
	})
}
