package oci

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
)

// setupTestCTF creates a temporary CTF directory and returns its path and CTF instance
func setupTestCTF(t *testing.T) ctf.CTF {
	t.Helper()
	tmpDir := t.TempDir()
	fs, err := filesystem.NewFS(tmpDir, os.O_RDWR|os.O_CREATE)
	require.NoError(t, err)
	return ctf.NewFileSystemCTF(fs)
}

func TestNewCTFComponentVersionStore(t *testing.T) {
	ctf := setupTestCTF(t)
	store := NewFromCTF(ctf)
	assert.NotNil(t, store)
	assert.Equal(t, ctf, store.archive)
}

func TestTargetResourceReference(t *testing.T) {
	ctf := setupTestCTF(t)
	store := NewFromCTF(ctf)
	ref := "test:reference"
	targetRef, err := store.TargetResourceReference(ref)
	assert.NoError(t, err)
	assert.Equal(t, ref, targetRef)
}

func TestStoreForReference(t *testing.T) {
	ctf := setupTestCTF(t)
	s := NewFromCTF(ctf)
	result, err := s.StoreForReference(t.Context(), "test:reference")
	assert.NoError(t, err)
	assert.Equal(t, "test", result.(*repositoryStore).repo)
}

func TestComponentVersionReference(t *testing.T) {
	ctf := setupTestCTF(t)
	store := NewFromCTF(ctf)
	ref := store.ComponentVersionReference("test-component", "v1.0.0")
	assert.Equal(t, "component-descriptors/test-component:v1.0.0", ref)
}

func TestFetch(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	blob := blob.NewDirectReadOnlyBlob(strings.NewReader(content))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, ctf.SaveBlob(ctx, blob))

	desc := ociImageSpecV1.Descriptor{
		Digest: digest.Digest(digestStr),
	}

	t.Run("successful fetch", func(t *testing.T) {
		reader, err := store.Fetch(ctx, desc)
		assert.NoError(t, err)
		assert.NotNil(t, reader)

		readContent, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.Equal(t, content, string(readContent))
	})

	t.Run("blob not found", func(t *testing.T) {
		nonExistentDesc := ociImageSpecV1.Descriptor{
			Digest: digest.FromString("testabc"),
		}
		reader, err := store.Fetch(ctx, nonExistentDesc)
		assert.Error(t, err)
		assert.Nil(t, reader)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestExists(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	blob := blob.NewDirectReadOnlyBlob(strings.NewReader(content))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, ctf.SaveBlob(ctx, blob))

	desc := ociImageSpecV1.Descriptor{
		Digest: digest.Digest(digestStr),
	}

	t.Run("blob exists", func(t *testing.T) {
		exists, err := store.Exists(ctx, desc)
		assert.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("blob does not exist", func(t *testing.T) {
		nonExistentDesc := ociImageSpecV1.Descriptor{
			Digest: digest.Digest("sha256:1234"),
		}
		exists, err := store.Exists(ctx, nonExistentDesc)
		assert.NoError(t, err)
		assert.False(t, exists)
	})
}

func TestPush(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	desc := ociImageSpecV1.Descriptor{
		Digest: digest.FromString(content),
		Size:   int64(len(content)),
	}

	t.Run("successful push", func(t *testing.T) {
		err := store.Push(ctx, desc, strings.NewReader(content))
		assert.NoError(t, err)

		// Verify the blob was saved
		exists, err := store.Exists(ctx, desc)
		assert.NoError(t, err)
		assert.True(t, exists)
	})
}

func TestResolve(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	reference := "test-repo:test-tag"
	content := "test"
	blob := blob.NewDirectReadOnlyBlob(strings.NewReader(content))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, ctf.SaveBlob(ctx, blob))

	// Create and set up the index
	index := v1.NewIndex()
	index.AddArtifact(v1.ArtifactMetadata{
		Repository: "test-repo",
		Tag:        "test-tag",
		Digest:     digestStr,
		MediaType:  ociImageSpecV1.MediaTypeImageManifest,
	})
	require.NoError(t, ctf.SetIndex(ctx, index))

	t.Run("successful resolve", func(t *testing.T) {
		desc, err := store.Resolve(ctx, reference)
		assert.NoError(t, err)
		assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
		assert.Equal(t, digest.Digest(digestStr), desc.Digest)
	})

	t.Run("invalid reference", func(t *testing.T) {
		desc, err := store.Resolve(ctx, "invalid")
		assert.Error(t, err)
		assert.Empty(t, desc)
	})

	t.Run("reference not found", func(t *testing.T) {
		desc, err := store.Resolve(ctx, "other-repo:other-tag")
		assert.Error(t, err)
		assert.Empty(t, desc)
	})
}

func TestTag(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	reference := "test-repo:test-tag"
	content := "test"
	blob := blob.NewDirectReadOnlyBlob(strings.NewReader(content))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, ctf.SaveBlob(ctx, blob))

	desc := ociImageSpecV1.Descriptor{
		Digest: digest.Digest(digestStr),
	}

	t.Run("successful tag", func(t *testing.T) {
		err := store.Tag(ctx, desc, reference)
		assert.NoError(t, err)

		// Verify the tag was created by resolving it
		resolvedDesc, err := store.Resolve(ctx, reference)
		assert.NoError(t, err)
		assert.Equal(t, desc.Digest, resolvedDesc.Digest)
	})

	t.Run("invalid reference", func(t *testing.T) {
		err := store.Tag(ctx, desc, "invalid")
		assert.Error(t, err)
	})
}
