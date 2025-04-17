package fetch

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
)

func TestSingleLayerLocalBlobFromManifestByIdentity(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	// Create a test manifest with a single layer
	layerContent := []byte("test layer content")
	layerDigest := digest.FromBytes(layerContent)
	manifest := &ociImageSpecV1.Manifest{
		Layers: []ociImageSpecV1.Descriptor{
			{
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
				Digest:    layerDigest,
				Size:      int64(len(layerContent)),
				Annotations: map[string]string{
					annotations.ArtifactAnnotationKey: `[{"kind": "resource", "identity": {"test": "value"}}]`,
				},
			},
		},
	}

	// Store the layer content
	err := store.Push(ctx, manifest.Layers[0], bytes.NewReader(layerContent))
	require.NoError(t, err)

	// Store the manifest
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestDigest := digest.FromBytes(manifestBytes)
	manifestDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      int64(len(manifestBytes)),
	}
	err = store.Push(ctx, manifestDesc, bytes.NewReader(manifestBytes))
	require.NoError(t, err)

	// Test successful case
	identity := map[string]string{
		"test": "value",
	}
	blob, err := SingleLayerLocalBlobFromManifestByIdentity(ctx, store, manifest, identity)
	require.NoError(t, err)
	assert.NotNil(t, blob)
	digest, _ := blob.Digest()
	assert.Equal(t, layerDigest.String(), digest)
	size := blob.Size()
	assert.Equal(t, int64(len(layerContent)), size)
	mediaType, _ := blob.MediaType()
	assert.Equal(t, "application/vnd.oci.image.layer.v1.tar+gzip", mediaType)

	// Test case where no matching layer is found
	identity = map[string]string{
		"nonexistent": "value",
	}
	_, err = SingleLayerLocalBlobFromManifestByIdentity(ctx, store, manifest, identity)
	assert.Error(t, err)
}

func TestSingleLayerManifestBlobFromIndex(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	// Create a test manifest
	layerContent := []byte("test layer content")
	layerDigest := digest.FromBytes(layerContent)
	manifest := ociImageSpecV1.Manifest{
		Layers: []ociImageSpecV1.Descriptor{
			{
				MediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
				Digest:    layerDigest,
				Size:      int64(len(layerContent)),
			},
		},
	}

	// Store the layer content
	err := store.Push(ctx, manifest.Layers[0], bytes.NewReader(layerContent))
	require.NoError(t, err)

	// Store the manifest
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestDigest := digest.FromBytes(manifestBytes)
	manifestDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      int64(len(manifestBytes)),
		Annotations: map[string]string{
			annotations.ArtifactAnnotationKey: `[{"kind": "resource", "identity": {"test": "value"}}]`,
		},
	}
	err = store.Push(ctx, manifestDesc, bytes.NewReader(manifestBytes))
	require.NoError(t, err)

	// Create a test index
	index := &ociImageSpecV1.Index{
		Manifests: []ociImageSpecV1.Descriptor{manifestDesc},
	}
	indexBytes, err := json.Marshal(index)
	require.NoError(t, err)
	indexDigest := digest.FromBytes(indexBytes)
	indexDesc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Digest:    indexDigest,
		Size:      int64(len(indexBytes)),
	}
	err = store.Push(ctx, indexDesc, bytes.NewReader(indexBytes))
	require.NoError(t, err)

	// Test successful case
	identity := map[string]string{
		"test": "value",
	}
	blob, err := SingleLayerManifestBlobFromIndex(ctx, store, index, identity)
	require.NoError(t, err)
	assert.NotNil(t, blob)

	// Test case where no matching manifest is found
	identity = map[string]string{
		"nonexistent": "value",
	}
	_, err = SingleLayerManifestBlobFromIndex(ctx, store, index, identity)
	assert.Error(t, err)
}

func TestLayerFromManifest(t *testing.T) {
	manifest := ociImageSpecV1.Manifest{
		Layers: []ociImageSpecV1.Descriptor{
			{
				MediaType: "layer1",
				Digest:    digest.Digest("sha256:123"),
			},
			{
				MediaType: "layer2",
				Digest:    digest.Digest("sha256:456"),
			},
		},
	}

	// Test successful case
	desc, err := LayerFromManifest(manifest, 0)
	require.NoError(t, err)
	assert.Equal(t, "layer1", desc.MediaType)
	assert.Equal(t, "sha256:123", desc.Digest.String())

	// Test out of bounds case
	_, err = LayerFromManifest(manifest, 2)
	assert.Error(t, err)

	// Test empty manifest case
	emptyManifest := ociImageSpecV1.Manifest{}
	_, err = LayerFromManifest(emptyManifest, 0)
	assert.Error(t, err)
}

func TestManifest(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	// Create a test manifest
	testManifest := ociImageSpecV1.Manifest{
		Config: ociImageSpecV1.Descriptor{
			MediaType: "config",
			Digest:    digest.Digest("sha256:config123"),
		},
		Layers: []ociImageSpecV1.Descriptor{
			{
				MediaType: "layer1",
				Digest:    digest.Digest("sha256:123"),
			},
		},
	}

	// Store the manifest
	manifestBytes, err := json.Marshal(testManifest)
	require.NoError(t, err)
	manifestDigest := digest.FromBytes(manifestBytes)
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    manifestDigest,
		Size:      int64(len(manifestBytes)),
	}
	err = store.Push(ctx, desc, bytes.NewReader(manifestBytes))
	require.NoError(t, err)

	// Test successful case
	manifest, err := Manifest(ctx, store, desc)
	require.NoError(t, err)
	assert.Equal(t, "config", manifest.Config.MediaType)
	assert.Equal(t, "sha256:config123", manifest.Config.Digest.String())
	assert.Len(t, manifest.Layers, 1)
	assert.Equal(t, "layer1", manifest.Layers[0].MediaType)
}
