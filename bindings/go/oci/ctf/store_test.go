package ctf

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/errdef"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/path"
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

func TestStoreForReference(t *testing.T) {
	ctf := setupTestCTF(t)
	s := NewFromCTF(ctf)
	result, err := s.StoreForReference(t.Context(), "test:reference")
	require.NoError(t, err)
	assert.Equal(t, "test", result.(*repository).repo)
}

func TestComponentVersionReference(t *testing.T) {
	ctf := setupTestCTF(t)
	store := NewFromCTF(ctf)
	ref := store.ComponentVersionReference(t.Context(), "ocm.software/test-component", "v1.0.0")
	assert.Equal(t, fmt.Sprintf("%s/%s/ocm.software/test-component:v1.0.0", wellKnownRegistryCTF, path.DefaultComponentDescriptorPath), ref)
}

func TestFetch(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	blob := inmemory.New(strings.NewReader(content))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, ctf.SaveBlob(ctx, blob))

	desc := ociImageSpecV1.Descriptor{
		Digest: digest.Digest(digestStr),
	}

	t.Run("successful fetch", func(t *testing.T) {
		reader, err := store.Fetch(ctx, desc)
		require.NoError(t, err)
		assert.NotNil(t, reader)

		readContent, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, content, string(readContent))
	})

	t.Run("blob not found", func(t *testing.T) {
		nonExistentDesc := ociImageSpecV1.Descriptor{
			Digest: digest.FromString("testabc"),
		}
		reader, err := store.Fetch(ctx, nonExistentDesc)
		require.Error(t, err)
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
	blob := inmemory.New(strings.NewReader(content))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, ctf.SaveBlob(ctx, blob))

	desc := ociImageSpecV1.Descriptor{
		Digest: digest.Digest(digestStr),
	}

	t.Run("blob exists", func(t *testing.T) {
		exists, err := store.Exists(ctx, desc)
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("blob does not exist", func(t *testing.T) {
		nonExistentDesc := ociImageSpecV1.Descriptor{
			Digest: digest.Digest("sha256:1234"),
		}
		exists, err := store.Exists(ctx, nonExistentDesc)
		require.NoError(t, err)
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
		require.NoError(t, err)

		// Verify the blob was saved
		exists, err := store.Exists(ctx, desc)
		require.NoError(t, err)
		assert.True(t, exists)
	})
}

func TestPushStreaming(t *testing.T) {
	ctx := t.Context()
	const mediaType = "application/octet-stream"
	content := []byte("streamed layer")
	want := digest.FromBytes(content)
	wantSHA512 := digest.SHA512.FromBytes(content)

	tests := []struct {
		name        string
		preexisting bool
		partial     ociImageSpecV1.Descriptor
		wantDigest  digest.Digest
		wantErr     string
	}{
		{name: "unknown digest and size", wantDigest: want},
		{name: "expected digest matches", partial: ociImageSpecV1.Descriptor{Digest: want}, wantDigest: want},
		{name: "expected digest and size match", partial: ociImageSpecV1.Descriptor{Digest: want, Size: int64(len(content))}, wantDigest: want},
		{name: "sha512 expected digest", partial: ociImageSpecV1.Descriptor{Digest: wantSHA512}, wantDigest: wantSHA512},
		{name: "expected digest mismatches", partial: ociImageSpecV1.Descriptor{Digest: digest.FromString("other")}, wantErr: "does not match expected digest"},
		{name: "expected size too large", partial: ociImageSpecV1.Descriptor{Size: int64(len(content) + 1)}, wantErr: "does not match expected size"},
		{name: "expected size too small", partial: ociImageSpecV1.Descriptor{Size: int64(len(content) - 1)}, wantErr: "does not match expected size"},
		{name: "already stored, unknown digest", preexisting: true, wantDigest: want},
		{name: "already stored, expected digest", preexisting: true, partial: ociImageSpecV1.Descriptor{Digest: want}, wantDigest: want},
		{name: "already stored, expected size mismatches", preexisting: true, partial: ociImageSpecV1.Descriptor{Digest: want, Size: int64(len(content) + 1)}, wantErr: "does not match expected size"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			archive := setupTestCTF(t)
			store, err := NewFromCTF(archive).StoreForReference(ctx, "test-repo:test-tag")
			r.NoError(err)
			if tc.preexisting {
				r.NoError(archive.SaveBlob(ctx, inmemory.New(bytes.NewReader(content))))
			}

			tc.partial.MediaType = mediaType
			desc, err := store.(*repository).PushStreaming(ctx, tc.partial, bytes.NewReader(content))

			var wantBlobs []string
			if tc.preexisting {
				wantBlobs = []string{want.String()}
			}
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
			} else {
				r.NoError(err)
				r.Equal(ociImageSpecV1.Descriptor{MediaType: mediaType, Digest: tc.wantDigest, Size: int64(len(content))}, desc)
				wantBlobs = []string{tc.wantDigest.String()}
			}
			blobs, err := archive.ListBlobs(ctx)
			if wantBlobs == nil {
				// The blobs directory may not even exist when nothing was stored.
				r.Empty(blobs)
				return
			}
			r.NoError(err)
			r.Equal(wantBlobs, blobs, "only the content-addressed blob may remain, no staged file")

			if tc.wantErr != "" {
				return
			}
			rc, err := store.Fetch(ctx, desc)
			r.NoError(err)
			t.Cleanup(func() { _ = rc.Close() })
			data, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(content, data)
		})
	}

	t.Run("manifest is not streamed", func(t *testing.T) {
		r := require.New(t)
		store, err := NewFromCTF(setupTestCTF(t)).StoreForReference(ctx, "test-repo:test-tag")
		r.NoError(err)

		data := bytes.NewReader(content)
		_, err = store.(*repository).PushStreaming(ctx, ociImageSpecV1.Descriptor{MediaType: ociImageSpecV1.MediaTypeImageManifest}, data)
		r.ErrorIs(err, remotestore.ErrStreamingUnavailable)
		r.Equal(len(content), data.Len(), "content must not be consumed")
	})
}

func TestResolve(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "localhost:5000/test-repo:v1.0.0")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	blob := inmemory.New(strings.NewReader(content))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, ctf.SaveBlob(ctx, blob))

	// Create and set up the index
	index := v1.NewIndex()
	index.AddArtifact(v1.ArtifactMetadata{
		Repository: "test-repo",
		Tag:        "v1.0.0",
		Digest:     digestStr,
		MediaType:  ociImageSpecV1.MediaTypeImageManifest,
	})
	index.AddArtifact(v1.ArtifactMetadata{
		Repository: "test-repo",
		Tag:        "1.0.0-T0",
		Digest:     digestStr,
		MediaType:  ociImageSpecV1.MediaTypeImageManifest,
	})
	require.NoError(t, ctf.SetIndex(ctx, index))

	expectedOkResolves := []string{
		"v1.0.0",
		"1.0.0-T0",
		"test-repo:1.0.0-T0",
		"test-repo:v1.0.0",
		digestStr,
		"test-repo@" + digestStr,
		"test-repo:v1.0.0@" + digestStr,
		"localhost:5000/test-repo:v1.0.0",
		"localhost:5000/test-repo:v1.0.0@" + digestStr,
	}

	for _, tc := range expectedOkResolves {
		t.Run(tc, func(t *testing.T) {
			desc, err := store.Resolve(ctx, tc)
			require.NoError(t, err)
			assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
			assert.Equal(t, digest.Digest(digestStr), desc.Digest)
		})
	}

	t.Run("invalid reference", func(t *testing.T) {
		desc, err := store.Resolve(ctx, "invalid")
		require.Error(t, err)
		assert.Empty(t, desc)
	})

	t.Run("unparseable reference that is not a tag", func(t *testing.T) {
		desc, err := store.Resolve(ctx, "localhost:5000/Test-Repo:v1.0.0")
		require.ErrorIs(t, err, errdef.ErrInvalidReference)
		assert.Empty(t, desc)
	})

	t.Run("reference not found", func(t *testing.T) {
		desc, err := store.Resolve(ctx, "other-repo:other-tag")
		require.Error(t, err)
		assert.Empty(t, desc)
	})
}

func TestTag(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	blob := inmemory.New(strings.NewReader(content))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, ctf.SaveBlob(ctx, blob))

	desc := ociImageSpecV1.Descriptor{
		Digest: digest.Digest(digestStr),
	}

	tests := []struct {
		name      string
		reference string
	}{
		{
			name:      "simple tag",
			reference: "test-tag",
		},
		{
			name:      "tag with upper-case letters",
			reference: "1.0.0-T0",
		},
		{
			name:      "tag as digest",
			reference: desc.Digest.String(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Tag(ctx, desc, tt.reference)
			require.NoError(t, err)

			// Verify the tag was created by resolving it
			resolvedDesc, err := store.Resolve(ctx, tt.reference)
			require.NoError(t, err)
			assert.Equal(t, desc.Digest, resolvedDesc.Digest)
		})
	}
}

func TestFetchReference(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	blob := inmemory.New(strings.NewReader(content))
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

	t.Run("successful fetch reference", func(t *testing.T) {
		desc, reader, err := store.(*repository).FetchReference(ctx, "test-tag")
		require.NoError(t, err)
		assert.NotNil(t, reader)
		assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
		assert.Equal(t, digest.Digest(digestStr), desc.Digest)

		readContent, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, content, string(readContent))
	})

	t.Run("fetch reference not found", func(t *testing.T) {
		desc, reader, err := store.(*repository).FetchReference(ctx, "nonexistent-tag")
		require.Error(t, err)
		assert.Nil(t, reader)
		assert.Empty(t, desc)
	})
}

func TestTags(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	blob := inmemory.New(strings.NewReader(content))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, ctf.SaveBlob(ctx, blob))

	// Create and set up the index with multiple tags
	index := v1.NewIndex()
	index.AddArtifact(v1.ArtifactMetadata{
		Repository: "test-repo",
		Tag:        "tag1",
		Digest:     digestStr,
		MediaType:  ociImageSpecV1.MediaTypeImageManifest,
	})
	index.AddArtifact(v1.ArtifactMetadata{
		Repository: "test-repo",
		Tag:        "tag2",
		Digest:     digestStr,
		MediaType:  ociImageSpecV1.MediaTypeImageManifest,
	})
	index.AddArtifact(v1.ArtifactMetadata{
		Repository: "other-repo",
		Tag:        "other-tag",
		Digest:     digestStr,
		MediaType:  ociImageSpecV1.MediaTypeImageManifest,
	})
	require.NoError(t, ctf.SetIndex(ctx, index))

	t.Run("list tags for repository", func(t *testing.T) {
		var tags []string
		err := store.(*repository).Tags(ctx, "", func(t []string) error {
			tags = t
			return nil
		})
		require.NoError(t, err)
		// Like OCI Image Layout, multiple tags can point to the same digest
		assert.ElementsMatch(t, []string{"tag1", "tag2"}, tags, "multiple tags for same digest should coexist")
	})
}

func TestPushWithManifest(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	desc := ociImageSpecV1.Descriptor{
		Digest:    digest.FromString(content),
		Size:      int64(len(content)),
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
	}

	t.Run("push manifest and verify tag", func(t *testing.T) {
		err := store.Push(ctx, desc, strings.NewReader(content))
		require.NoError(t, err)

		// Verify the blob was saved
		exists, err := store.Exists(ctx, desc)
		require.NoError(t, err)
		assert.True(t, exists)

		// Verify the digest is resolvable
		resolvedDesc, err := store.Resolve(ctx, desc.Digest.String())
		require.NoError(t, err)
		assert.Equal(t, desc.Digest, resolvedDesc.Digest)
	})
}

func TestResolveWithRegistry(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "registry.example.com/test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	blob := inmemory.New(strings.NewReader(content))
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

	t.Run("resolve with registry prefix", func(t *testing.T) {
		desc, err := store.Resolve(ctx, "test-tag")
		require.NoError(t, err)
		assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
		assert.Equal(t, digest.Digest(digestStr), desc.Digest)
	})
}

func TestResolveWithEmptyMediaType(t *testing.T) {
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	content := "test"
	blob := inmemory.New(strings.NewReader(content))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, ctf.SaveBlob(ctx, blob))

	// Create and set up the index with empty MediaType to simulate old CTF
	index := v1.NewIndex()
	index.AddArtifact(v1.ArtifactMetadata{
		Repository: "test-repo",
		Tag:        "test-tag",
		Digest:     digestStr,
		MediaType:  "", // Set to Empty on purpose to signify test.
	})
	require.NoError(t, ctf.SetIndex(ctx, index))

	t.Run("resolve with empty media type defaults to image manifest", func(t *testing.T) {
		desc, err := store.Resolve(ctx, "test-tag")
		require.NoError(t, err)
		assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
		assert.Equal(t, digest.Digest(digestStr), desc.Digest)
	})
}

func TestCompatibility(t *testing.T) {
	r := require.New(t)
	for _, tc := range []struct {
		path string
	}{
		{
			path: "testdata/compatibility/01/transport-archive",
		},
		{
			path: "testdata/compatibility/01/transport-archive.tar",
		},
		{
			path: "testdata/compatibility/01/transport-archive.tar.gz",
		},
	} {
		t.Run(tc.path, func(t *testing.T) {
			archive, _, err := ctf.OpenCTFByFileExtension(t.Context(), ctf.OpenCTFOptions{
				Path:    tc.path,
				Flag:    ctf.O_RDONLY,
				TempDir: t.TempDir(),
			})
			r.NoError(err)
			repo, err := oci.NewRepository(
				WithCTF(NewFromCTF(archive)),
				oci.WithCreator("I am the Creator"),
			)
			r.NoError(err)
			cv, err := repo.GetComponentVersion(t.Context(), "github.com/acme.org/helloworld", "1.0.0")
			r.NoError(err)
			r.Equal("github.com/acme.org/helloworld", cv.Component.Name)

			r.Len(cv.Component.Resources, 1)

			b, res, err := repo.GetLocalResource(t.Context(), cv.Component.Name, cv.Component.Version, cv.Component.Resources[0].ToIdentity())
			r.NoError(err)
			r.NotNil(b)
			r.NotNil(res)

			rc, err := b.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() {
				r.NoError(rc.Close())
			})

			data, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal("test", string(data))
		})
	}
}

// TestConcurrentBlobOperations tests thread safety of blob operations
func TestConcurrentBlobOperations(t *testing.T) {
	t.Parallel()
	ctf := setupTestCTF(t)
	provider := NewFromCTF(ctf)
	store, err := provider.StoreForReference(t.Context(), "test-repo:test-tag")
	require.NoError(t, err)

	ctx := t.Context()
	numGoroutines := 10
	numBlobsPerGoroutine := 5

	// Test concurrent operations on the same blob
	t.Run("concurrent same blob", func(t *testing.T) {
		t.Parallel()
		// Create a test blob
		content := "concurrent test content"
		blobDigest := digest.FromString(content).String()
		desc := ociImageSpecV1.Descriptor{
			MediaType: "application/octet-stream",
			Size:      int64(len(content)),
			Digest:    digest.Digest(blobDigest),
		}

		var wg sync.WaitGroup

		// Launch multiple goroutines performing operations on the same blob
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()

				// Push operation
				err := store.Push(ctx, desc, bytes.NewReader([]byte(content)))
				assert.NoError(t, err, "goroutine %d: push should succeed", id)

				// Fetch operation
				reader, err := store.Fetch(ctx, desc)
				assert.NoError(t, err, "goroutine %d: fetch should succeed", id)
				assert.NotNil(t, reader, "goroutine %d: reader should not be nil", id)

				data, err := io.ReadAll(reader)
				reader.Close()
				assert.NoError(t, err, "goroutine %d: read should succeed", id)
				assert.Equal(t, content, string(data), "goroutine %d: content should match", id)

				// Exists operation
				exists, err := store.Exists(ctx, desc)
				assert.NoError(t, err, "goroutine %d: exists check should succeed", id)
				assert.True(t, exists, "goroutine %d: blob should exist", id)
			}(i)
		}

		wg.Wait()
	})

	// Test concurrent operations on different blobs
	t.Run("concurrent different blobs", func(t *testing.T) {
		t.Parallel()
		var wg sync.WaitGroup

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < numBlobsPerGoroutine; j++ {
					content := fmt.Sprintf("content-goroutine-%d-blob-%d", goroutineID, j)
					blobDigest := digest.FromString(content).String()
					desc := ociImageSpecV1.Descriptor{
						MediaType: "application/test",
						Size:      int64(len(content)),
						Digest:    digest.Digest(blobDigest),
					}

					// Push
					err := store.Push(ctx, desc, bytes.NewReader([]byte(content)))
					assert.NoError(t, err, "goroutine %d blob %d: push should succeed", goroutineID, j)

					// Fetch
					reader, err := store.Fetch(ctx, desc)
					assert.NoError(t, err, "goroutine %d blob %d: fetch should succeed", goroutineID, j)
					assert.NotNil(t, reader, "goroutine %d blob %d: reader should not be nil", goroutineID, j)

					data, err := io.ReadAll(reader)
					reader.Close()
					assert.NoError(t, err, "goroutine %d blob %d: read should succeed", goroutineID, j)
					assert.Equal(t, content, string(data), "goroutine %d blob %d: content should match", goroutineID, j)

					// Exists
					exists, err := store.Exists(ctx, desc)
					assert.NoError(t, err, "goroutine %d blob %d: exists check should succeed", goroutineID, j)
					assert.True(t, exists, "goroutine %d blob %d: blob should exist", goroutineID, j)
				}
			}(i)
		}

		wg.Wait()
	})
}

func TestUntag(t *testing.T) {
	archive := setupTestCTF(t)
	provider := NewFromCTF(archive)
	store, err := provider.StoreForReference(t.Context(), "test-repo:v1.0.0")
	require.NoError(t, err)

	ctx := t.Context()
	blob := inmemory.New(strings.NewReader("manifest content"))
	digestStr, known := blob.Digest()
	require.True(t, known)
	require.NoError(t, archive.SaveBlob(ctx, blob))

	idx := v1.NewIndex()
	idx.AddArtifact(v1.ArtifactMetadata{Repository: "test-repo", Tag: "v1.0.0", Digest: digestStr})
	idx.AddArtifact(v1.ArtifactMetadata{Repository: "test-repo", Tag: "latest", Digest: digestStr})
	require.NoError(t, archive.SetIndex(ctx, idx))

	require.NoError(t, store.(*repository).Untag(ctx, "latest"))

	var tags []string
	require.NoError(t, store.(*repository).Tags(ctx, "", func(ts []string) error {
		tags = ts
		return nil
	}))
	assert.ElementsMatch(t, []string{"v1.0.0"}, tags, "only the semver tag should remain")

	_, err = store.Resolve(ctx, "latest")
	require.Error(t, err, "removed tag must not resolve")

	// Untag only removes the tag pointer — the blob itself must remain.
	blobs, err := archive.ListBlobs(ctx)
	require.NoError(t, err)
	assert.Contains(t, blobs, digestStr, "underlying blob must survive untagging")
}

func TestUntag_NotFound(t *testing.T) {
	archive := setupTestCTF(t)
	provider := NewFromCTF(archive)
	store, err := provider.StoreForReference(t.Context(), "test-repo:v1.0.0")
	require.NoError(t, err)

	err = store.(*repository).Untag(t.Context(), "nonexistent")
	assert.ErrorIs(t, err, errdef.ErrNotFound)
}

func TestUntag_LastTag_KeepsBlob(t *testing.T) {
	// Per the content.Untagger contract, untagging is purely nominal: even when
	// the removed tag was the only index entry for the manifest, the blob stays.
	archive := setupTestCTF(t)
	provider := NewFromCTF(archive)
	ctx := t.Context()

	content := []byte("manifest content")
	manifestDigest := digest.FromBytes(content)
	require.NoError(t, archive.SaveBlob(ctx, inmemory.New(bytes.NewReader(content))))

	idx := v1.NewIndex()
	idx.AddArtifact(v1.ArtifactMetadata{
		Repository: "test-repo",
		Tag:        "latest",
		Digest:     manifestDigest.String(),
		MediaType:  ociImageSpecV1.MediaTypeImageManifest,
	})
	require.NoError(t, archive.SetIndex(ctx, idx))

	store, err := provider.StoreForReference(ctx, "test-repo:latest")
	require.NoError(t, err)
	require.NoError(t, store.(*repository).Untag(ctx, "latest"))

	_, err = store.Resolve(ctx, "latest")
	require.Error(t, err, "removed tag must not resolve")

	blobs, err := archive.ListBlobs(ctx)
	require.NoError(t, err)
	assert.Contains(t, blobs, manifestDigest.String(), "blob must not be deleted by untagging")
}
