package tar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/content/memory"
	orasoci "oras.land/oras-go/v2/content/oci"
	"oras.land/oras-go/v2/errdef"

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

// TestCopyOCILayoutWithIndex_TransferReferrer verifies that a referrer carried
// in the source layout (subject points back at the artifact root) lands in dst
// alongside the artifact. ExtendedCopyGraph walks predecessors of the root via
// src.Predecessors and copies each as its own root.
func TestCopyOCILayoutWithIndex_TransferReferrer(t *testing.T) {
	const artifactType = "application/test.referrer.v1+json"
	layoutBytes := buildLayoutWithSourceReferrer(t, artifactType)

	dst := memory.New()
	returnedTop, err := CopyOCILayoutWithIndex(t.Context(), dst, &testReadOnlyBlob{data: layoutBytes}, CopyOCILayoutWithIndexOptions{
		MutateParentFunc: func(d *ociImageSpecV1.Descriptor) error { return nil },
	})
	require.NoError(t, err)

	ok, err := dst.Exists(t.Context(), returnedTop)
	require.NoError(t, err)
	assert.True(t, ok, "artifact root must be in dst")

	predecessors, err := dst.Predecessors(t.Context(), returnedTop)
	require.NoError(t, err)
	require.Len(t, predecessors, 1, "the source-carried referrer must land in dst as a predecessor of the root")
	assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, predecessors[0].MediaType)
}

// TestCopyOCILayoutWithIndex_NoReferrer verifies that a layout without any
// referrer copies only the artifact root and its successors — dst has no
// predecessors of the root.
func TestCopyOCILayoutWithIndex_NoReferrer(t *testing.T) {
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

// buildLayoutWithSourceReferrer produces an OCI layout (one layer + manifest,
// tagged) that also carries a referrer manifest of artifactType in its index —
// i.e. what an incoming layout looks like on transfer once the source referrer
// has been pulled into it.
func buildLayoutWithSourceReferrer(t *testing.T, artifactType string) []byte {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	var buf bytes.Buffer
	w, err := NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	r.NoError(err)

	layerData := []byte("layer content")
	layer := content.NewDescriptorFromBytes(ociImageSpecV1.MediaTypeImageLayer, layerData)
	r.NoError(w.Push(ctx, layer, bytes.NewReader(layerData)))

	main, err := oras.PackManifest(ctx, w, oras.PackManifestVersion1_1, "application/artifact", oras.PackManifestOptions{
		Layers: []ociImageSpecV1.Descriptor{layer},
	})
	r.NoError(err)

	empty := ociImageSpecV1.DescriptorEmptyJSON
	if err := w.Push(ctx, empty, bytes.NewReader(empty.Data)); err != nil && !errors.Is(err, errdef.ErrAlreadyExists) {
		r.NoError(err)
	}
	refBody, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Config:       empty,
		Layers:       []ociImageSpecV1.Descriptor{empty},
		Subject:      &main,
	})
	r.NoError(err)
	refDesc := ociImageSpecV1.Descriptor{
		MediaType:    ociImageSpecV1.MediaTypeImageManifest,
		ArtifactType: artifactType,
		Digest:       digest.FromBytes(refBody),
		Size:         int64(len(refBody)),
	}
	r.NoError(w.Push(ctx, refDesc, bytes.NewReader(refBody)))

	r.NoError(w.Tag(ctx, main, "latest"))
	r.NoError(w.Close())
	return buf.Bytes()
}

// TestCopyOCILayoutWithIndex_AnnotationSurvivalAndIdempotency uses a layout-backed
// destination (oras-go's oci.Store) so MutateParentFunc-injected annotations are
// observable in the destination's index.json — memory.New() drops annotations on
// Push. The same call repeated converges (idempotent re-run).
func TestCopyOCILayoutWithIndex_AnnotationSurvivalAndIdempotency(t *testing.T) {
	r := require.New(t)
	const artifactType = "application/test.referrer.v1+json"
	layoutBytes := buildLayoutWithSourceReferrer(t, artifactType)

	dstDir := t.TempDir()
	dst, err := orasoci.New(dstDir)
	r.NoError(err)

	mutate := func(d *ociImageSpecV1.Descriptor) error {
		if d.Annotations == nil {
			d.Annotations = map[string]string{}
		}
		d.Annotations["software.ocm/test"] = "value"
		return nil
	}

	returnedTop, err := CopyOCILayoutWithIndex(t.Context(), dst, &testReadOnlyBlob{data: layoutBytes}, CopyOCILayoutWithIndexOptions{
		MutateParentFunc: mutate,
	})
	r.NoError(err)

	t.Run("annotations survive into the destination layout's index.json", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(dstDir, "index.json"))
		require.NoError(t, err)
		var idx ociImageSpecV1.Index
		require.NoError(t, json.Unmarshal(raw, &idx))
		var found *ociImageSpecV1.Descriptor
		for i := range idx.Manifests {
			if idx.Manifests[i].Digest == returnedTop.Digest {
				found = &idx.Manifests[i]
				break
			}
		}
		require.NotNil(t, found, "the copied root must be present in the destination index.json")
		assert.Equal(t, "value", found.Annotations["software.ocm/test"])
	})

	t.Run("transferred referrer lands as a predecessor of the root", func(t *testing.T) {
		predecessors, err := dst.Predecessors(t.Context(), returnedTop)
		require.NoError(t, err)
		require.Len(t, predecessors, 1)
	})

	t.Run("repeated copy is idempotent", func(t *testing.T) {
		_, err := CopyOCILayoutWithIndex(t.Context(), dst, &testReadOnlyBlob{data: layoutBytes}, CopyOCILayoutWithIndexOptions{
			MutateParentFunc: mutate,
		})
		require.NoError(t, err)

		predecessors, err := dst.Predecessors(t.Context(), returnedTop)
		require.NoError(t, err)
		assert.Len(t, predecessors, 1, "re-run must converge on the same referrer")
	})
}

// TestCopyOCILayoutWithIndex_IdempotencyWhenRootExistsButReferrerMissing verifies
// the per-root copy semantics of ExtendedCopyGraph: when the artifact root is
// already in dst but a referrer is not, the missing referrer still lands. This
// is the regression check that the old CopyReferrerRoots second-pass handled.
func TestCopyOCILayoutWithIndex_IdempotencyWhenRootExistsButReferrerMissing(t *testing.T) {
	r := require.New(t)
	const artifactType = "application/test.referrer.v1+json"

	// First copy the artifact with no referrer attached, so only the root and
	// layer land in dst.
	layoutWithRef := buildLayoutWithSourceReferrer(t, artifactType)
	dst := memory.New()
	// Strip the referrer manifest from the source by feeding only the artifact
	// root into a fresh layout — emulates a prior transfer that did not carry
	// the referrer.
	layoutNoRef := stripReferrerFromLayout(t, layoutWithRef)
	rootNoRef, err := CopyOCILayoutWithIndex(t.Context(), dst, &testReadOnlyBlob{data: layoutNoRef}, CopyOCILayoutWithIndexOptions{
		MutateParentFunc: func(*ociImageSpecV1.Descriptor) error { return nil },
	})
	r.NoError(err)

	// Now copy a layout that carries a referrer for the same root. The root is
	// already in dst; the referrer must still land via ExtendedCopyGraph's
	// per-predecessor copy.
	rootWithRef, err := CopyOCILayoutWithIndex(t.Context(), dst, &testReadOnlyBlob{data: layoutWithRef}, CopyOCILayoutWithIndexOptions{
		MutateParentFunc: func(*ociImageSpecV1.Descriptor) error { return nil },
	})
	r.NoError(err)
	r.Equal(rootNoRef.Digest, rootWithRef.Digest, "both layouts pack to the same root")

	predecessors, err := dst.Predecessors(t.Context(), rootWithRef)
	r.NoError(err)
	assert.Len(t, predecessors, 1, "missing referrer must still land even when the root already exists in dst")
}

// stripReferrerFromLayout copies layoutBytes into a fresh layout containing
// only the tagged root manifest and its successors, dropping any referrer
// manifests that point at the root.
func stripReferrerFromLayout(t *testing.T, layoutBytes []byte) []byte {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()

	src, err := ReadOCILayout(ctx, &testReadOnlyBlob{data: layoutBytes})
	r.NoError(err)
	defer src.Close()

	var root ociImageSpecV1.Descriptor
	for _, m := range src.Index.Manifests {
		if m.Annotations[ociImageSpecV1.AnnotationRefName] != "" {
			root = m
			break
		}
	}
	r.NotEmpty(root.Digest, "tagged root must exist in source layout")

	var buf bytes.Buffer
	w, err := NewOCILayoutWriterWithTempFile(&buf, t.TempDir())
	r.NoError(err)
	r.NoError(oras.CopyGraph(ctx, src, w, root, oras.CopyGraphOptions{}))
	r.NoError(w.Tag(ctx, root, "latest"))
	r.NoError(w.Close())
	return buf.Bytes()
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

func (s *invalidStore) Predecessors(ctx context.Context, desc ociImageSpecV1.Descriptor) ([]ociImageSpecV1.Descriptor, error) {
	return nil, assert.AnError
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
