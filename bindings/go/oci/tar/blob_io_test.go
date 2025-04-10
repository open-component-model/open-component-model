package tar

import (
	"bytes"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
)

func TestCopyOCILayout(t *testing.T) {
	// Create a test OCI layout with a manifest and a blob
	testBlobData := []byte("test blob content")
	desc := content.NewDescriptorFromBytes("application/json", testBlobData)
	var buf bytes.Buffer
	ociLayout := NewOCILayoutWriter(&buf)
	require.NoError(t, ociLayout.Push(t.Context(), desc, bytes.NewReader(testBlobData)))

	manifest, err := oras.PackManifest(t.Context(), ociLayout, oras.PackManifestVersion1_1, "application/artifact", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{desc},
	})
	require.NoError(t, err)

	require.NoError(t, ociLayout.Close())

	// Create a file store
	store := memory.New()

	// Copy the OCI layout with a tag
	opts := CopyOCILayoutOptions{
		MutateIndexFunc: func(desc *ociImageSpecV1.Descriptor) error {
			desc.Annotations = map[string]string{
				"some": "annotation",
			}
			return nil
		},
	}
	index, err := CopyOCILayout(t.Context(), store, &testBlob{data: buf.Bytes()}, opts)
	require.NoError(t, err)

	idxExists, err := store.Exists(t.Context(), index)
	require.NoError(t, err)
	assert.True(t, idxExists)

	manifestExists, err := store.Exists(t.Context(), manifest)
	require.NoError(t, err)
	assert.True(t, manifestExists)

	// Verify the blob exists
	blobExists, err := store.Exists(t.Context(), desc)
	require.NoError(t, err)
	assert.True(t, blobExists)
}
