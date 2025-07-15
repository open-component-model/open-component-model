package tar

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
)

func TestTopLevelArtifacts(t *testing.T) {
	tests := []struct {
		name         string
		setupFetcher func(t *testing.T) (content.Fetcher, []ociImageSpecV1.Descriptor)
		expected     []ociImageSpecV1.Descriptor
	}{
		{
			name: "artifact referencing another is not top-level",
			setupFetcher: func(t *testing.T) (content.Fetcher, []ociImageSpecV1.Descriptor) {
				fetcher := memory.New()

				child := ociImageSpecV1.Manifest{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
				}
				childData, err := json.Marshal(child)
				require.NoError(t, err)
				childDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, childData)
				require.NoError(t, fetcher.Push(t.Context(), childDesc, bytes.NewReader(childData)))

				parent := ociImageSpecV1.Index{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Manifests: []ociImageSpecV1.Descriptor{
						childDesc,
					},
				}
				parentData, err := json.Marshal(parent)
				require.NoError(t, err)
				parentDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, parentData)
				require.NoError(t, fetcher.Push(t.Context(), parentDesc, bytes.NewReader(parentData)))
				return fetcher, []ociImageSpecV1.Descriptor{
					childDesc,
					parentDesc,
				}
			},
			expected: []ociImageSpecV1.Descriptor{
				{
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Digest:    digest.Digest("sha256:1a3216b8c91e38ffb62fceac27eb6de195e3fd36576b2118c544d3a394162a98"),
					Size:      240,
				},
			},
		},
		{
			name: "single artifact is always top-level",
			setupFetcher: func(t *testing.T) (content.Fetcher, []ociImageSpecV1.Descriptor) {
				fetcher := memory.New()

				manifest := ociImageSpecV1.Manifest{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
				}
				manifestData, err := json.Marshal(manifest)
				require.NoError(t, err)
				manifestDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, manifestData)
				require.NoError(t, fetcher.Push(t.Context(), manifestDesc, bytes.NewReader(manifestData)))

				return fetcher, []ociImageSpecV1.Descriptor{manifestDesc}
			},
			expected: []ociImageSpecV1.Descriptor{
				{
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
					Digest:    digest.Digest("sha256:f3b9d1fb0df87f63f2da0913b9da8c663935b66d04342bf6dc66bffd6351fc81"),
					Size:      137,
				},
			},
		},
		{
			name: "empty candidates returns empty result",
			setupFetcher: func(t *testing.T) (content.Fetcher, []ociImageSpecV1.Descriptor) {
				fetcher := memory.New()
				return fetcher, []ociImageSpecV1.Descriptor{}
			},
			expected: []ociImageSpecV1.Descriptor{},
		},
		{
			name: "multiple independent artifacts are all top-level",
			setupFetcher: func(t *testing.T) (content.Fetcher, []ociImageSpecV1.Descriptor) {
				fetcher := memory.New()

				// Create two independent manifests
				manifest1 := ociImageSpecV1.Manifest{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
				}
				manifest1Data, err := json.Marshal(manifest1)
				require.NoError(t, err)
				manifest1Desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, manifest1Data)
				require.NoError(t, fetcher.Push(t.Context(), manifest1Desc, bytes.NewReader(manifest1Data)))

				manifest2 := ociImageSpecV1.Manifest{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
					Config: ociImageSpecV1.Descriptor{
						MediaType: "application/vnd.oci.image.config.v1+json",
						Digest:    digest.FromString("config"),
						Size:      7,
					},
				}
				manifest2Data, err := json.Marshal(manifest2)
				require.NoError(t, err)
				manifest2Desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, manifest2Data)
				require.NoError(t, fetcher.Push(t.Context(), manifest2Desc, bytes.NewReader(manifest2Data)))

				return fetcher, []ociImageSpecV1.Descriptor{manifest1Desc, manifest2Desc}
			},
			expected: []ociImageSpecV1.Descriptor{
				{
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
					Digest:    digest.Digest("sha256:f3b9d1fb0df87f63f2da0913b9da8c663935b66d04342bf6dc66bffd6351fc81"),
					Size:      137,
				},
				{
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
					Digest:    digest.Digest("sha256:8f5aed45d813dd435240590bac1905d134263306067a969e309a6f12dd6d02b1"),
					Size:      248,
				},
			},
		},
		{
			name: "complex dependency chain - only root is top-level",
			setupFetcher: func(t *testing.T) (content.Fetcher, []ociImageSpecV1.Descriptor) {
				fetcher := memory.New()

				// Create a chain: leaf -> middle -> root
				leaf := ociImageSpecV1.Manifest{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
				}
				leafData, err := json.Marshal(leaf)
				require.NoError(t, err)
				leafDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, leafData)
				require.NoError(t, fetcher.Push(t.Context(), leafDesc, bytes.NewReader(leafData)))

				middle := ociImageSpecV1.Index{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Manifests: []ociImageSpecV1.Descriptor{
						leafDesc,
					},
				}
				middleData, err := json.Marshal(middle)
				require.NoError(t, err)
				middleDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, middleData)
				require.NoError(t, fetcher.Push(t.Context(), middleDesc, bytes.NewReader(middleData)))

				root := ociImageSpecV1.Index{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Manifests: []ociImageSpecV1.Descriptor{
						middleDesc,
					},
				}
				rootData, err := json.Marshal(root)
				require.NoError(t, err)
				rootDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, rootData)
				require.NoError(t, fetcher.Push(t.Context(), rootDesc, bytes.NewReader(rootData)))

				return fetcher, []ociImageSpecV1.Descriptor{
					leafDesc,
					middleDesc,
					rootDesc,
				}
			},
			expected: []ociImageSpecV1.Descriptor{
				{
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Digest:    digest.Digest("sha256:c49ecbbc49bb85e06df5f6f401ad4231c0d6853e6123ac4dce3909f6b22e8292"),
					Size:      237,
				},
			},
		},
		{
			name: "multiple roots with shared dependencies",
			setupFetcher: func(t *testing.T) (content.Fetcher, []ociImageSpecV1.Descriptor) {
				fetcher := memory.New()

				// Create a shared dependency
				shared := ociImageSpecV1.Manifest{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageManifest,
				}
				sharedData, err := json.Marshal(shared)
				require.NoError(t, err)
				sharedDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageManifest, sharedData)
				require.NoError(t, fetcher.Push(t.Context(), sharedDesc, bytes.NewReader(sharedData)))

				// Create two independent roots that both reference the shared dependency
				root1 := ociImageSpecV1.Index{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Manifests: []ociImageSpecV1.Descriptor{
						sharedDesc,
					},
					Annotations: map[string]string{
						"example.com/annotation": "root1",
					},
				}
				root1Data, err := json.Marshal(root1)
				require.NoError(t, err)
				root1Desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, root1Data)
				require.NoError(t, fetcher.Push(t.Context(), root1Desc, bytes.NewReader(root1Data)))

				root2 := ociImageSpecV1.Index{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Manifests: []ociImageSpecV1.Descriptor{
						sharedDesc,
					},
					Annotations: map[string]string{
						"example.com/annotation": "root2",
					},
				}
				root2Data, err := json.Marshal(root2)
				require.NoError(t, err)
				root2Desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, root2Data)
				require.NoError(t, fetcher.Push(t.Context(), root2Desc, bytes.NewReader(root2Data)))

				return fetcher, []ociImageSpecV1.Descriptor{
					sharedDesc,
					root1Desc,
					root2Desc,
				}
			},
			expected: []ociImageSpecV1.Descriptor{
				{
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Digest:    digest.Digest("sha256:bea74a61e4d48e9da7b68cc7fb8d2ca259b5c1c00ed294e4826285f3658f48cb"),
					Size:      289,
				},
				{
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Digest:    digest.Digest("sha256:547b3e78671b30aea4097f6135ee6f27347d772bcd8bbf65dc53442251f1b466"),
					Size:      289,
				},
			},
		},
		{
			name: "circular reference - all artifacts are top-level",
			setupFetcher: func(t *testing.T) (content.Fetcher, []ociImageSpecV1.Descriptor) {
				fetcher := memory.New()

				// Create two manifests that reference each other (circular dependency)
				manifest1 := ociImageSpecV1.Index{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Manifests: []ociImageSpecV1.Descriptor{
						{
							MediaType: ociImageSpecV1.MediaTypeImageManifest,
							Digest:    digest.FromString("manifest2"),
							Size:      100,
						},
					},
				}
				manifest1Data, err := json.Marshal(manifest1)
				require.NoError(t, err)
				manifest1Desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, manifest1Data)
				require.NoError(t, fetcher.Push(t.Context(), manifest1Desc, bytes.NewReader(manifest1Data)))

				manifest2 := ociImageSpecV1.Index{
					Versioned: specs.Versioned{
						SchemaVersion: 2,
					},
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Manifests: []ociImageSpecV1.Descriptor{
						{
							MediaType: ociImageSpecV1.MediaTypeImageManifest,
							Digest:    digest.FromString("manifest1"),
							Size:      100,
						},
					},
				}
				manifest2Data, err := json.Marshal(manifest2)
				require.NoError(t, err)
				manifest2Desc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, manifest2Data)
				require.NoError(t, fetcher.Push(t.Context(), manifest2Desc, bytes.NewReader(manifest2Data)))

				return fetcher, []ociImageSpecV1.Descriptor{manifest1Desc, manifest2Desc}
			},
			expected: []ociImageSpecV1.Descriptor{
				{
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Digest:    digest.Digest("sha256:b66dfa52009bec8a3e96b123696b4813db64c682c7ebc3171a184754aae64823"),
					Size:      240,
				},
				{
					MediaType: ociImageSpecV1.MediaTypeImageIndex,
					Digest:    digest.Digest("sha256:77eef83adc32ba68fa657e451c256ec21d7df09177ecbaa147e2e7b36babbee8"),
					Size:      240,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fetcher, candidates := tt.setupFetcher(t)
			require.Equal(t, tt.expected, TopLevelArtifacts(t.Context(), fetcher, candidates))
		})
	}
}
