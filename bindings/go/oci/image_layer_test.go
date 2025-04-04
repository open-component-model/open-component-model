package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content/memory"

	v1 "ocm.software/open-component-model/bindings/go/oci/access/v1"
)

func setupStore(t *testing.T) Store {
	return memory.New()
}

func pushContent(t *testing.T, store Store, mediaType string, content []byte) ociImageSpecV1.Descriptor {
	desc := ociImageSpecV1.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(content),
		Size:      int64(len(content)),
	}
	err := store.Push(context.Background(), desc, bytes.NewReader(content))
	require.NoError(t, err)
	return desc
}

func TestGetOCIImageLayerRecursively(t *testing.T) {
	targetContent := []byte("test content")
	targetDigest := digest.FromBytes(targetContent)

	tests := []struct {
		name          string
		setup         func(Store) ociImageSpecV1.Descriptor
		inputLayer    *v1.OCIImageLayer
		expectedDesc  ociImageSpecV1.Descriptor
		expectedError string
	}{
		{
			name: "direct match",
			setup: func(store Store) ociImageSpecV1.Descriptor {
				return pushContent(t, store, "test", targetContent)
			},
			inputLayer: &v1.OCIImageLayer{
				Digest: targetDigest,
			},
			expectedDesc: ociImageSpecV1.Descriptor{
				MediaType: "test",
				Digest:    targetDigest,
				Size:      int64(len(targetContent)),
			},
		},
		{
			name: "match in manifest layers",
			setup: func(store Store) ociImageSpecV1.Descriptor {
				layerDesc := pushContent(t, store, "test", targetContent)

				manifest := ociImageSpecV1.Manifest{
					Layers: []ociImageSpecV1.Descriptor{layerDesc},
				}
				manifestBytes, err := json.Marshal(manifest)
				require.NoError(t, err)

				return pushContent(t, store, ociImageSpecV1.MediaTypeImageManifest, manifestBytes)
			},
			inputLayer: &v1.OCIImageLayer{
				Digest: targetDigest,
			},
			expectedDesc: ociImageSpecV1.Descriptor{
				MediaType: "test",
				Digest:    targetDigest,
				Size:      int64(len(targetContent)),
			},
		},
		{
			name: "match in nested index",
			setup: func(store Store) ociImageSpecV1.Descriptor {
				// Create and push the target layer
				layerDesc := pushContent(t, store, "test", targetContent)

				// Create and push the manifest containing the layer
				manifest := ociImageSpecV1.Manifest{
					Layers: []ociImageSpecV1.Descriptor{layerDesc},
				}
				manifestBytes, err := json.Marshal(manifest)
				require.NoError(t, err)
				manifestDesc := pushContent(t, store, ociImageSpecV1.MediaTypeImageManifest, manifestBytes)

				// Create and push the index containing the manifest
				index := ociImageSpecV1.Index{
					Manifests: []ociImageSpecV1.Descriptor{manifestDesc},
				}
				indexBytes, err := json.Marshal(index)
				require.NoError(t, err)
				return pushContent(t, store, ociImageSpecV1.MediaTypeImageIndex, indexBytes)
			},
			inputLayer: &v1.OCIImageLayer{
				Digest: targetDigest,
			},
			expectedDesc: ociImageSpecV1.Descriptor{
				MediaType: "test",
				Digest:    targetDigest,
				Size:      int64(len(targetContent)),
			},
		},
		{
			name: "layer not found in manifest",
			setup: func(store Store) ociImageSpecV1.Descriptor {
				manifest := ociImageSpecV1.Manifest{
					Layers: []ociImageSpecV1.Descriptor{
						{
							MediaType: "test",
							Digest:    digest.FromString("different"),
							Size:      100,
						},
					},
				}
				manifestBytes, err := json.Marshal(manifest)
				require.NoError(t, err)
				return pushContent(t, store, ociImageSpecV1.MediaTypeImageManifest, manifestBytes)
			},
			inputLayer: &v1.OCIImageLayer{
				Digest: targetDigest,
			},
			expectedError: fmt.Sprintf("layer %s not found", targetDigest),
		},
		{
			name: "unsupported media type",
			setup: func(store Store) ociImageSpecV1.Descriptor {
				return ociImageSpecV1.Descriptor{
					MediaType: "unsupported",
					Digest:    digest.FromString("unsupported"),
				}
			},
			inputLayer: &v1.OCIImageLayer{
				Digest: targetDigest,
			},
			expectedError: fmt.Sprintf("layer %s not found", targetDigest),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := setupStore(t)
			inputDesc := tt.setup(store)

			desc, err := getOCIImageLayerRecursively(context.Background(), store, inputDesc, tt.inputLayer)

			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedDesc, desc)
			}
		})
	}
}

func TestGetOCIImageLayerRecursively_ConcurrentSearches(t *testing.T) {
	store := setupStore(t)
	targetContent := []byte("test content")
	targetDigest := digest.FromBytes(targetContent)

	// Create and push the target layer
	layerDesc := pushContent(t, store, "test", targetContent)

	// Create a large index with multiple manifests
	manifests := make([]ociImageSpecV1.Descriptor, 10)
	for i := range manifests {
		// Create manifest with a unique config for each iteration
		manifest := ociImageSpecV1.Manifest{
			Versioned: specs.Versioned{
				SchemaVersion: 2,
			},
			Config: ociImageSpecV1.Descriptor{
				MediaType: "application/vnd.oci.image.config.v1+json",
				Digest:    digest.FromString(fmt.Sprintf("config-%d", i)),
				Size:      100,
			},
		}

		// Add the target layer only to the last manifest
		if i == 9 {
			manifest.Layers = []ociImageSpecV1.Descriptor{layerDesc}
		}

		manifestBytes, err := json.Marshal(manifest)
		require.NoError(t, err)

		manifests[i] = pushContent(t, store, ociImageSpecV1.MediaTypeImageManifest, manifestBytes)
	}

	// Create and push the index
	index := ociImageSpecV1.Index{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		Manifests: manifests,
	}
	indexBytes, err := json.Marshal(index)
	require.NoError(t, err)
	indexDesc := pushContent(t, store, ociImageSpecV1.MediaTypeImageIndex, indexBytes)

	desc, err := getOCIImageLayerRecursively(context.Background(), store, indexDesc, &v1.OCIImageLayer{
		Digest: targetDigest,
	})

	require.NoError(t, err)
	assert.Equal(t, targetDigest, desc.Digest)
	assert.Equal(t, "test", desc.MediaType)
	assert.Equal(t, int64(len(targetContent)), desc.Size)
}

func TestGetOCIImageLayerRecursively_ContextCancellation(t *testing.T) {
	store := setupStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	targetContent := []byte("test content")
	targetDigest := digest.FromBytes(targetContent)

	// Create and push a manifest
	manifest := ociImageSpecV1.Manifest{
		Layers: []ociImageSpecV1.Descriptor{
			{
				MediaType: "test",
				Digest:    targetDigest,
				Size:      int64(len(targetContent)),
			},
		},
	}
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	manifestDesc := pushContent(t, store, ociImageSpecV1.MediaTypeImageManifest, manifestBytes)

	// Create and push an index
	index := ociImageSpecV1.Index{
		Manifests: []ociImageSpecV1.Descriptor{manifestDesc},
	}
	indexBytes, err := json.Marshal(index)
	require.NoError(t, err)
	indexDesc := pushContent(t, store, ociImageSpecV1.MediaTypeImageIndex, indexBytes)

	// Cancel context before searching
	cancel()

	_, err = getOCIImageLayerRecursively(ctx, store, indexDesc, &v1.OCIImageLayer{
		Digest: targetDigest,
	})

	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}
