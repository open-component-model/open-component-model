package ctf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
	"ocm.software/open-component-model/bindings/go/oci"
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

var logHandler *mockLogHandler

// mockLogHandler is a mock slog.Handler for testing
type mockLogHandler struct {
	mock.Mock
	mu sync.Mutex
}

func (m *mockLogHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (m *mockLogHandler) Handle(_ context.Context, record slog.Record) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Called(record.Level, record.Message)
	return nil
}

func (m *mockLogHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return m
}

func (m *mockLogHandler) WithGroup(_ string) slog.Handler {
	return m
}

func TestMain(m *testing.M) {
	logHandler = &mockLogHandler{}
	slog.SetDefault(slog.New(logHandler))
	logHandler.On("Handle", mock.AnythingOfType("slog.Level"), mock.AnythingOfType("string")).Return(nil)

	exitCode := m.Run()

	os.Exit(exitCode)
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
	assert.NoError(t, err)
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

	t.Run("close called multiple times is safe and logs error", func(t *testing.T) {
		reader, err := store.Fetch(ctx, desc)
		require.NoError(t, err)
		require.NotNil(t, reader)

		// Call Close multiple times - all should succeed without panic
		// The sync.OnceValue ensures RUnlock and rc.Close() are only called once
		err = reader.Close()
		assert.NoError(t, err, "first close should succeed")

		// Second call logs warning but not fail
		err = reader.Close()
		assert.NoError(t, err, "second close should be safe (logs warning)")

		// Verify the warning was logged exactly once
		logHandler.AssertCalled(t, "Handle", slog.LevelError, "Close called multiple times on locked reader.")
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
	require.NoError(t, ctf.SetIndex(ctx, index))

	expectedOkResolves := []string{
		"v1.0.0",
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
			assert.NoError(t, err)
			assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
			assert.Equal(t, digest.Digest(digestStr), desc.Digest)
		})
	}

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
			name:      "tag as digest",
			reference: desc.Digest.String(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := store.Tag(ctx, desc, tt.reference)
			assert.NoError(t, err)

			// Verify the tag was created by resolving it
			resolvedDesc, err := store.Resolve(ctx, tt.reference)
			assert.NoError(t, err)
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
		assert.NoError(t, err)
		assert.NotNil(t, reader)
		assert.Equal(t, ociImageSpecV1.MediaTypeImageManifest, desc.MediaType)
		assert.Equal(t, digest.Digest(digestStr), desc.Digest)

		readContent, err := io.ReadAll(reader)
		assert.NoError(t, err)
		assert.Equal(t, content, string(readContent))
	})

	t.Run("fetch reference not found", func(t *testing.T) {
		desc, reader, err := store.(*repository).FetchReference(ctx, "nonexistent-tag")
		assert.Error(t, err)
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
		assert.NoError(t, err)
		assert.ElementsMatch(t, []string{"tag2"}, tags, "a retag should not return the old tag")
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
		assert.NoError(t, err)

		// Verify the blob was saved
		exists, err := store.Exists(ctx, desc)
		assert.NoError(t, err)
		assert.True(t, exists)

		// Verify the digest is resolvable
		resolvedDesc, err := store.Resolve(ctx, desc.Digest.String())
		assert.NoError(t, err)
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
		assert.NoError(t, err)
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
		assert.NoError(t, err)
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
				require.NoError(t, err, "goroutine %d: push should succeed", id)

				// Fetch operation
				reader, err := store.Fetch(ctx, desc)
				require.NoError(t, err, "goroutine %d: fetch should succeed", id)
				require.NotNil(t, reader, "goroutine %d: reader should not be nil", id)

				data, err := io.ReadAll(reader)
				reader.Close()
				require.NoError(t, err, "goroutine %d: read should succeed", id)
				require.Equal(t, content, string(data), "goroutine %d: content should match", id)

				// Exists operation
				exists, err := store.Exists(ctx, desc)
				require.NoError(t, err, "goroutine %d: exists check should succeed", id)
				require.True(t, exists, "goroutine %d: blob should exist", id)
			}(i)
		}

		wg.Wait()
	})

	// Test concurrent operations on different blobs
	t.Run("concurrent different blobs", func(t *testing.T) {
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
					require.NoError(t, err, "goroutine %d blob %d: push should succeed", goroutineID, j)

					// Fetch
					reader, err := store.Fetch(ctx, desc)
					require.NoError(t, err, "goroutine %d blob %d: fetch should succeed", goroutineID, j)
					require.NotNil(t, reader, "goroutine %d blob %d: reader should not be nil", goroutineID, j)

					data, err := io.ReadAll(reader)
					reader.Close()
					require.NoError(t, err, "goroutine %d blob %d: read should succeed", goroutineID, j)
					require.Equal(t, content, string(data), "goroutine %d blob %d: content should match", goroutineID, j)

					// Exists
					exists, err := store.Exists(ctx, desc)
					require.NoError(t, err, "goroutine %d blob %d: exists check should succeed", goroutineID, j)
					require.True(t, exists, "goroutine %d blob %d: blob should exist", goroutineID, j)
				}
			}(i)
		}

		wg.Wait()
	})
}
