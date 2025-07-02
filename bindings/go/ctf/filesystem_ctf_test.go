package ctf_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/ctf"
	v1 "ocm.software/open-component-model/bindings/go/ctf/index/v1"
)

func Test_FileSystemCTF_BasicOperations(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	tmpDir := t.TempDir()

	// Create a new FileSystemCTF
	fs, err := ctf.OpenCTFFromOSPath(tmpDir, ctf.O_RDWR)
	r.NoError(err)

	// Test Format
	r.Equal(ctf.FormatDirectory, fs.Format())

	// Test GetIndex on empty CTF
	idx, err := fs.GetIndex(ctx)
	r.NoError(err)
	r.NotNil(idx)

	// Test SetIndex
	newIdx := v1.NewIndex()
	err = fs.SetIndex(ctx, newIdx)
	r.NoError(err)

	// Test SaveBlob
	testData := []byte("test blob data")
	testBlob := inmemory.New(bytes.NewReader(testData))
	err = fs.SaveBlob(ctx, testBlob)
	r.NoError(err)

	// Test ListBlobs
	blobs, err := fs.ListBlobs(ctx)
	r.NoError(err)
	r.Len(blobs, 1)

	// Test GetBlob
	digest, _ := testBlob.Digest()
	blob, err := fs.GetBlob(ctx, digest)
	r.NoError(err)
	r.NotNil(blob)

	// Test reading blob content
	reader, err := blob.ReadCloser()
	r.NoError(err)
	defer reader.Close()

	content, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(testData, content)

	// Test DeleteBlob
	err = fs.DeleteBlob(ctx, digest)
	r.NoError(err)

	// Verify blob is deleted
	blobs, err = fs.ListBlobs(ctx)
	r.NoError(err)
	r.Len(blobs, 0)
}

func Test_FileSystemCTF_MemFS(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()

	content := []byte("test content")
	dig := digest.FromBytes(content)
	f, err := ctf.ToBlobFileName(dig.String())
	r.NoError(err)

	idx := v1.NewIndex()
	idx.AddArtifact(v1.ArtifactMetadata{
		Repository: "test",
		Tag:        "test",
		MediaType:  "test",
	})
	idxData, err := json.Marshal(idx)
	r.NoError(err)

	mfs := fstest.MapFS{
		v1.ArtifactIndexFileName: &fstest.MapFile{
			Data: idxData,
		},
		filepath.Join(ctf.BlobsDirectoryName, f): &fstest.MapFile{
			Data:    content,
			Mode:    0644,
			ModTime: time.Now(),
		},
	}

	archive := ctf.NewFileSystemCTF(mfs)

	b, err := archive.GetBlob(ctx, dig.String())
	r.NoError(err)
	r.NotNil(b)

	// read only fs => no write or delete should be possible
	r.Error(archive.SaveBlob(ctx, b))
	r.Error(archive.DeleteBlob(ctx, dig.String()))

	stream, err := b.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(stream.Close())
	})
	data, err := io.ReadAll(stream)
	r.NoError(err)

	r.Equal(content, data)

	idxFromArchive, err := archive.GetIndex(ctx)
	r.NoError(err)
	r.NotNil(idxFromArchive)

	r.Equal(idx.GetArtifacts(), idxFromArchive.GetArtifacts())
}

func Test_FileSystemCTF_ErrorCases(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	tmpDir := t.TempDir()

	// Create a new FileSystemCTF
	fs, err := ctf.OpenCTFFromOSPath(tmpDir, ctf.O_RDONLY)
	r.NoError(err)

	// Test SetIndex on read-only CTF
	newIdx := v1.NewIndex()
	err = fs.SetIndex(ctx, newIdx)
	r.Error(err)

	// Test SaveBlob on read-only CTF
	testBlob := blob.NewDirectReadOnlyBlob(bytes.NewReader([]byte("test")))
	err = fs.SaveBlob(ctx, testBlob)
	r.Error(err)

	// Test DeleteBlob on read-only CTF
	err = fs.DeleteBlob(ctx, "sha256:test")
	r.Error(err)

	// Test GetBlob with non-existent digest
	blob, err := fs.GetBlob(ctx, "sha256:non-existent")
	r.Error(err)
	r.Nil(blob)
}

func Test_FileSystemCTF_ConcurrentOperations(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	tmpDir := t.TempDir()

	// Create a new FileSystemCTF
	fs, err := ctf.OpenCTFFromOSPath(tmpDir, ctf.O_RDWR)
	r.NoError(err)

	// Create multiple blobs concurrently
	blobs := make([]blob.ReadOnlyBlob, 10)
	for i := 0; i < 10; i++ {
		data := []byte(fmt.Sprintf("test blob %d", i))
		blobs[i] = inmemory.New(bytes.NewReader(data))
	}

	// Save blobs concurrently
	errGroup := errgroup.Group{}
	for _, b := range blobs {
		blob := b // Create new variable to avoid closure issues
		errGroup.Go(func() error {
			return fs.SaveBlob(ctx, blob)
		})
	}
	r.NoError(errGroup.Wait())

	// Verify all blobs were saved
	savedBlobs, err := fs.ListBlobs(ctx)
	r.NoError(err)
	r.Len(savedBlobs, 10)
}

func Test_FileSystemCTF_IndexOperations(t *testing.T) {
	ctx := t.Context()
	r := require.New(t)
	tmpDir := t.TempDir()

	// Create a new FileSystemCTF
	fs, err := ctf.OpenCTFFromOSPath(tmpDir, ctf.O_RDWR)
	r.NoError(err)

	// Create and set an index with artifacts
	idx := v1.NewIndex()
	testBlob := inmemory.New(bytes.NewReader([]byte("test")))
	digest, _ := testBlob.Digest()
	err = fs.SaveBlob(ctx, testBlob)
	r.NoError(err)

	idx.AddArtifact(v1.ArtifactMetadata{
		Repository: "test-repo",
		Tag:        "latest",
		Digest:     digest,
		MediaType:  "application/json",
	})

	err = fs.SetIndex(ctx, idx)
	r.NoError(err)

	// Verify index was saved correctly
	savedIdx, err := fs.GetIndex(ctx)
	r.NoError(err)
	r.Len(savedIdx.GetArtifacts(), 1)
	artifact := savedIdx.GetArtifacts()[0]
	r.Equal("test-repo", artifact.Repository)
	r.Equal("latest", artifact.Tag)
	r.Equal(digest, artifact.Digest)
}

func Test_FileSystemCTF_FileSystemOperations(t *testing.T) {
	r := require.New(t)
	tmpDir := t.TempDir()

	// Create a new FileSystemCTF
	fs, err := ctf.OpenCTFFromOSPath(tmpDir, ctf.O_RDWR)
	r.NoError(err)

	// Test FS() returns the correct filesystem
	underlyingFS := fs.FS()
	r.NotNil(underlyingFS)

	// Test writing a file directly to the filesystem
	testFile := "test.txt"
	testContent := []byte("test content")
	err = os.WriteFile(filepath.Join(tmpDir, testFile), testContent, 0644)
	r.NoError(err)

	// Verify the file exists in the filesystem
	_, err = os.Stat(filepath.Join(tmpDir, testFile))
	r.NoError(err)
}

type mockBlob struct {
	blob.ReadOnlyBlob
	digest string
	known  bool
	size   int64
	reader io.ReadCloser
	err    error
}

func (m *mockBlob) Digest() (string, bool) {
	return m.digest, m.known
}

func (m *mockBlob) Size() int64 {
	return m.size
}

func (m *mockBlob) ReadCloser() (io.ReadCloser, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.reader, nil
}

func TestFileSystemCTF_SaveBlob_ErrorCases(t *testing.T) {
	tests := []struct {
		name    string
		blob    *mockBlob
		wantErr string
	}{
		{
			name: "blob not digest aware",
			blob: &mockBlob{
				digest: "",
				known:  false,
			},
			wantErr: "blob does not have a digest that can be used to save it",
		},
		{
			name: "digest not known",
			blob: &mockBlob{
				digest: "sha256:123",
				known:  false,
			},
			wantErr: "blob does not have a digest that can be used to save it",
		},
		{
			name: "read error",
			blob: &mockBlob{
				digest: "sha256:123",
				known:  true,
				size:   100,
				err:    fmt.Errorf("read error"),
			},
			wantErr: "unable to read blob: read error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a temporary directory for the test
			dir := t.TempDir()
			fs, err := filesystem.NewFS(dir, os.O_RDWR)
			require.NoError(t, err)
			ctf := ctf.NewFileSystemCTF(fs)

			err = ctf.SaveBlob(t.Context(), tt.blob)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
