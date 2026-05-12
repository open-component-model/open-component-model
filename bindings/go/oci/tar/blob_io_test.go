package tar

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
)

func TestCopyOCILayout(t *testing.T) {
	t.Run("with manifest", func(t *testing.T) {
		testBlobData := []byte("test blob content")
		desc := content.NewDescriptorFromBytes("application/json", testBlobData)
		var buf bytes.Buffer
		ociLayout, err := NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
		require.NoError(t, err)
		require.NoError(t, ociLayout.Push(t.Context(), desc, bytes.NewReader(testBlobData)))

		manifest, err := oras.PackManifest(t.Context(), ociLayout, oras.PackManifestVersion1_1, "application/artifact", oras.PackManifestOptions{
			Layers: []ociImageSpecV1.Descriptor{desc},
		})
		require.NoError(t, err)
		require.NoError(t, ociLayout.Close())

		store := memory.New()
		opts := CopyOCILayoutWithIndexOptions{
			MutateParentFunc: func(desc *ociImageSpecV1.Descriptor) error {
				desc.Annotations = map[string]string{"some": "annotation"}
				return nil
			},
		}
		index, err := CopyOCILayoutWithIndex(t.Context(), store, &testReadOnlyBlob{data: buf.Bytes()}, opts)
		require.NoError(t, err)

		idxExists, err := store.Exists(t.Context(), index)
		require.NoError(t, err)
		assert.True(t, idxExists)

		manifestExists, err := store.Exists(t.Context(), manifest)
		require.NoError(t, err)
		assert.True(t, manifestExists)

		blobExists, err := store.Exists(t.Context(), desc)
		require.NoError(t, err)
		assert.True(t, blobExists)
	})

	t.Run("with top-level index but not all lower level manifests are in top level index", func(t *testing.T) {
		testBlobData := []byte("test blob content")
		desc := content.NewDescriptorFromBytes("application/json", testBlobData)
		var buf bytes.Buffer
		ociLayout, err := NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
		require.NoError(t, err)
		require.NoError(t, ociLayout.Push(t.Context(), desc, bytes.NewReader(testBlobData)))

		manifest, err := oras.PackManifest(t.Context(), ociLayout, oras.PackManifestVersion1_1, "application/artifact", oras.PackManifestOptions{
			Layers: []ociImageSpecV1.Descriptor{desc},
		})
		require.NoError(t, err)

		// build top-level index referring to manifest
		index := ociImageSpecV1.Index{
			Manifests: []ociImageSpecV1.Descriptor{manifest},
		}
		indexBytes, err := json.Marshal(index)
		require.NoError(t, err)
		indexDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, indexBytes)
		require.NoError(t, ociLayout.Push(t.Context(), indexDesc, bytes.NewReader(indexBytes)))

		// emulate empty manifest list since tooling such as docker does not write every manifest into the top level index
		ociLayout.index.Manifests = ociLayout.index.Manifests[1:]

		require.NoError(t, ociLayout.Close())

		store := memory.New()
		opts := CopyOCILayoutWithIndexOptions{
			MutateParentFunc: func(desc *ociImageSpecV1.Descriptor) error {
				desc.Annotations = map[string]string{"top": "index"}
				return nil
			},
		}
		topIndex, err := CopyOCILayoutWithIndex(t.Context(), store, &testReadOnlyBlob{data: buf.Bytes()}, opts)
		require.NoError(t, err)

		ok, err := store.Exists(t.Context(), topIndex)
		require.NoError(t, err)
		assert.True(t, ok)

		ok, err = store.Exists(t.Context(), manifest)
		require.NoError(t, err)
		assert.True(t, ok)

		ok, err = store.Exists(t.Context(), desc)
		require.NoError(t, err)
		assert.True(t, ok)
	})
}

func TestCopyToOCILayoutInMemory(t *testing.T) {
	// Create a test OCI layout with a manifest and a blob
	testBlobData := []byte("test blob content")
	desc := content.NewDescriptorFromBytes("application/json", testBlobData)

	// Create a source store with the blob
	src := memory.New()
	require.NoError(t, src.Push(t.Context(), desc, bytes.NewReader(testBlobData)))

	// Create a manifest
	manifest, err := oras.PackManifest(t.Context(), src, oras.PackManifestVersion1_1, "application/artifact", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{desc},
	})
	require.NoError(t, err)

	// Test copying with tags
	testCopy(t, err, src, manifest, manifest)
}

// TestCopyToOCILayoutInMemoryBasedOnIndex tests the CopyToOCILayoutInMemory function with an index as source
func TestCopyToOCILayoutInMemoryBasedOnIndex(t *testing.T) {
	// Create a test OCI layout with a manifest and a blob
	testBlobData := []byte("test blob content")
	desc := content.NewDescriptorFromBytes("application/json", testBlobData)

	// Create a source store with the blob
	src := memory.New()
	require.NoError(t, src.Push(t.Context(), desc, bytes.NewReader(testBlobData)))

	// Create a manifest
	manifest, err := oras.PackManifest(t.Context(), src, oras.PackManifestVersion1_1, "application/artifact", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{desc},
	})
	require.NoError(t, err)

	index := ociImageSpecV1.Index{
		Manifests: []ociImageSpecV1.Descriptor{
			manifest,
		},
	}
	indexSerialized, err := json.Marshal(index)
	require.NoError(t, err)
	indexDesc := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageIndex, indexSerialized)

	require.NoError(t, src.Push(t.Context(), indexDesc, bytes.NewReader(indexSerialized)))

	// Test copying with tags
	testCopy(t, err, src, indexDesc, manifest)
}

func testCopy(t *testing.T, err error, src *memory.Store, indexDesc ociImageSpecV1.Descriptor, manifest ociImageSpecV1.Descriptor) {
	t.Helper()
	opts := CopyToOCILayoutOptions{
		Tags: []string{"latest", "v1"},
	}
	blob, err := CopyToOCILayoutInMemory(t.Context(), src, indexDesc, opts)
	require.NoError(t, err)
	assert.NotNil(t, blob)

	mediaType, ok := blob.MediaType()
	assert.True(t, ok)
	assert.Equal(t, layout.MediaTypeOCIImageLayoutTarGzipV1, mediaType)

	digest, ok := blob.Digest()
	assert.True(t, ok)
	assert.NotEmpty(t, digest)

	// Test copying without tags
	opts = CopyToOCILayoutOptions{}
	blob, err = CopyToOCILayoutInMemory(t.Context(), src, manifest, opts)
	require.NoError(t, err)
	assert.NotNil(t, blob)
}

func TestCopyToOCILayoutInMemory_ErrorCases(t *testing.T) {
	// Test with invalid source store
	invalidStore := &invalidStore{}
	opts := CopyToOCILayoutOptions{}
	b, err := CopyToOCILayoutInMemory(t.Context(), invalidStore, ociImageSpecV1.Descriptor{}, opts)
	assert.NoError(t, err)
	rc, err := b.ReadCloser()
	assert.Error(t, err)
	assert.Nil(t, rc)

	// Test with invalid descriptor
	src := memory.New()
	b, err = CopyToOCILayoutInMemory(t.Context(), src, ociImageSpecV1.Descriptor{}, opts)
	assert.NoError(t, err)
	rc, err = b.ReadCloser()
	assert.Error(t, err)
	assert.Nil(t, rc)
}

// buildSingleLayerOCILayout produces an OCI layout (one layer + one manifest)
// for tests that need a real artifact to feed into CopyOCILayoutWithIndex.
func buildSingleLayerOCILayout(t *testing.T) (layoutBytes []byte, root, layer ociImageSpecV1.Descriptor) {
	t.Helper()
	layerData := []byte("layer content")
	layer = content.NewDescriptorFromBytes("application/octet-stream", layerData)
	var buf bytes.Buffer
	w, err := NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	require.NoError(t, err)
	require.NoError(t, w.Push(t.Context(), layer, bytes.NewReader(layerData)))
	root, err = oras.PackManifest(t.Context(), w, oras.PackManifestVersion1_1, "application/artifact", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layer},
	})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return buf.Bytes(), root, layer
}

// TestCopyOCILayoutWithIndex_ReferrersFunc verifies the ReferrersFunc hook:
// the callback's descriptors land in dst alongside the artifact, and a
// referrer's Subject back-reference to root does not cause CopyGraph to loop
// back.
func TestCopyOCILayoutWithIndex_ReferrersFunc(t *testing.T) {
	layoutBytes, rootDesc, layerDesc := buildSingleLayerOCILayout(t)

	var receivedRoot, referrerDesc ociImageSpecV1.Descriptor
	referrerFn := func(ctx context.Context, top ociImageSpecV1.Descriptor) ([]Referrer, error) {
		receivedRoot = top
		emptyDesc := ociImageSpecV1.DescriptorEmptyJSON
		body, err := json.Marshal(ociImageSpecV1.Manifest{
			Versioned:    specs.Versioned{SchemaVersion: 2},
			MediaType:    ociImageSpecV1.MediaTypeImageManifest,
			ArtifactType: "application/test.referrer.v1+json",
			Config:       emptyDesc,
			Layers:       []ociImageSpecV1.Descriptor{emptyDesc},
			Subject:      &top,
		})
		if err != nil {
			return nil, err
		}
		referrerDesc = ociImageSpecV1.Descriptor{
			MediaType:    ociImageSpecV1.MediaTypeImageManifest,
			ArtifactType: "application/test.referrer.v1+json",
			Digest:       digest.FromBytes(body),
			Size:         int64(len(body)),
		}
		return []Referrer{
			{Descriptor: referrerDesc, Raw: body},
			{Descriptor: emptyDesc, Raw: []byte("{}")},
		}, nil
	}

	dst := memory.New()
	returnedTop, err := CopyOCILayoutWithIndex(t.Context(), dst, &testReadOnlyBlob{data: layoutBytes}, CopyOCILayoutWithIndexOptions{
		MutateParentFunc: func(d *ociImageSpecV1.Descriptor) error { return nil },
		ReferrersFunc:    []ReferrersFunc{referrerFn},
	})
	require.NoError(t, err)

	assert.Equal(t, rootDesc.Digest, receivedRoot.Digest)

	for _, d := range []ociImageSpecV1.Descriptor{returnedTop, layerDesc, referrerDesc} {
		ok, err := dst.Exists(t.Context(), d)
		require.NoError(t, err)
		assert.Truef(t, ok, "%s must be in dst", d.Digest)
	}

	predecessors, err := dst.Predecessors(t.Context(), returnedTop)
	require.NoError(t, err)
	require.Len(t, predecessors, 1, "subject back-reference must yield exactly one referrer")
	assert.Equal(t, referrerDesc.Digest, predecessors[0].Digest)
}

// TestCopyOCILayoutWithIndex_NilReferrersFunc verifies that a nil callback
// leaves the pre-ReferrersFunc behaviour intact: only the artifact lands.
func TestCopyOCILayoutWithIndex_NilReferrersFunc(t *testing.T) {
	layoutBytes, rootDesc, _ := buildSingleLayerOCILayout(t)

	dst := memory.New()
	returnedTop, err := CopyOCILayoutWithIndex(t.Context(), dst, &testReadOnlyBlob{data: layoutBytes}, CopyOCILayoutWithIndexOptions{
		MutateParentFunc: func(d *ociImageSpecV1.Descriptor) error { return nil },
	})
	require.NoError(t, err)
	assert.Equal(t, rootDesc.Digest, returnedTop.Digest)

	predecessors, err := dst.Predecessors(t.Context(), returnedTop)
	require.NoError(t, err)
	assert.Empty(t, predecessors)
}

func TestCopyOCILayoutWithIndex_ErrorCases(t *testing.T) {
	// Test with invalid blob
	store := memory.New()
	opts := CopyOCILayoutWithIndexOptions{}
	_, err := CopyOCILayoutWithIndex(t.Context(), store, &testReadOnlyBlob{data: []byte("invalid")}, opts)
	assert.Error(t, err)

	// Test with invalid store
	_, err = CopyOCILayoutWithIndex(t.Context(), &invalidStore{}, &testReadOnlyBlob{data: []byte("test")}, opts)
	assert.Error(t, err)
}

// invalidStore is a store that always returns errors
type invalidStore struct {
	content.Storage
}

func (s *invalidStore) Exists(ctx context.Context, desc ociImageSpecV1.Descriptor) (bool, error) {
	return false, assert.AnError
}

func (s *invalidStore) Fetch(ctx context.Context, desc ociImageSpecV1.Descriptor) (io.ReadCloser, error) {
	return nil, assert.AnError
}

func (s *invalidStore) Push(ctx context.Context, desc ociImageSpecV1.Descriptor, content io.Reader) error {
	return assert.AnError
}

// testReadOnlyBlob implements blob.ReadOnlyBlob for testing
type testReadOnlyBlob struct {
	data []byte
}

func (b *testReadOnlyBlob) Get() ([]byte, error) {
	return b.data, nil
}

func (b *testReadOnlyBlob) Reader() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func (b *testReadOnlyBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func (b *testReadOnlyBlob) Close() error {
	return nil
}
