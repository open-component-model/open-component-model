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
	r.Empty(blobs)
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
			Mode:    0o644,
			ModTime: time.Now(),
		},
	}

	archive := ctf.NewFileSystemCTF(mfs)

	b, err := archive.GetBlob(ctx, dig.String())
	r.NoError(err)
	r.NotNil(b)

	// read only fs => no write or delete should be possible; saving content that is already
	// stored writes nothing and is allowed
	r.NoError(archive.SaveBlob(ctx, b))
	r.Error(archive.SaveBlob(ctx, inmemory.New(bytes.NewReader([]byte("new content")))))
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
	testBlob := inmemory.New(bytes.NewReader([]byte("test")))
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

// A staged blob is invisible until published, publishing keeps an already stored blob,
// and discarding leaves nothing behind.
func TestFileSystemCTF_StageBlob(t *testing.T) {
	ctx := t.Context()
	content := []byte("staged content")
	dig := digest.FromBytes(content).String()

	for _, tc := range []struct {
		name        string
		preexisting bool
		publish     bool
		wantBlobs   []string
	}{
		{name: "published", publish: true, wantBlobs: []string{dig}},
		{name: "published over stored blob", preexisting: true, publish: true, wantBlobs: []string{dig}},
		{name: "discarded", wantBlobs: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			archive, err := ctf.OpenCTFFromOSPath(dir, ctf.O_RDWR)
			r.NoError(err)
			if tc.preexisting {
				r.NoError(archive.SaveBlob(ctx, inmemory.New(bytes.NewReader(content))))
			}

			staged, err := archive.StageBlob(ctx, inmemory.New(bytes.NewReader(content)))
			r.NoError(err)
			r.Equal(dig, staged.Digest())
			if !tc.preexisting {
				_, err = archive.GetBlob(ctx, dig)
				r.Error(err, "a staged blob must not be visible before it is published")
			}
			if tc.publish {
				r.NoError(staged.Publish())
			}
			r.NoError(staged.Discard())

			entries, err := os.ReadDir(filepath.Join(dir, ctf.BlobsDirectoryName))
			r.NoError(err)
			names := []string{}
			for _, e := range entries {
				names = append(names, ctf.ToDigest(e.Name()))
			}
			r.Equal(tc.wantBlobs, names, "only published blobs may remain, no staged file")
			if !tc.publish {
				return
			}
			stored, err := archive.GetBlob(ctx, dig)
			r.NoError(err)
			rc, err := stored.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() { _ = rc.Close() })
			data, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(content, data)
		})
	}
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

	idx.AddArtifact(v1.ArtifactMetadata{
		Repository: "test-repo2",
		Tag:        "latest",
		Digest:     digest,
		MediaType:  "application/json",
	})
	err = fs.SetIndex(ctx, idx)
	r.NoError(err)

	// Verify index was saved correctly
	savedIdx, err = fs.GetIndex(ctx)
	r.NoError(err)
	r.Len(savedIdx.GetArtifacts(), 2)
	artifact = savedIdx.GetArtifacts()[1]
	r.Equal("test-repo2", artifact.Repository)
	r.Equal("latest", artifact.Tag)
	r.Equal(digest, artifact.Digest)

	// make sure smaller index is saved correctly
	r.NoError(fs.SetIndex(ctx, v1.NewIndex()))
	// Verify index was saved correctly
	savedIdx, err = fs.GetIndex(ctx)
	r.NoError(err)
	r.Empty(savedIdx.GetArtifacts())
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
	err = os.WriteFile(filepath.Join(tmpDir, testFile), testContent, 0o644)
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
		blob    blob.ReadOnlyBlob
		wantErr string
	}{
		{
			name:    "blob not digest aware",
			blob:    struct{ blob.ReadOnlyBlob }{},
			wantErr: "blob does not have a digest that can be used to save it",
		},
		{
			name: "digest not reported after read",
			blob: &mockBlob{
				size:   blob.SizeUnknown,
				reader: io.NopCloser(bytes.NewReader([]byte("content"))),
			},
			wantErr: "blob does not have a digest that can be used to save it",
		},
		{
			name: "read error",
			blob: &mockBlob{
				digest: digest.FromString("unreadable").String(),
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

// failingCloseReader fails on Close, like a verifying reader whose content does not
// match its digest.
type failingCloseReader struct{ io.Reader }

func (failingCloseReader) Close() error { return fmt.Errorf("digest mismatch") }

// TestFileSystemCTF_SaveBlob_KeepsExistingBlobOnFailedVerification checks that content
// failing verification on close neither replaces nor deletes a blob already stored under
// the same digest, and leaves no staged file behind.
func TestFileSystemCTF_SaveBlob_KeepsExistingBlobOnFailedVerification(t *testing.T) {
	r := require.New(t)
	dir := t.TempDir()
	archive, err := ctf.OpenCTFFromOSPath(dir, ctf.O_RDWR)
	r.NoError(err)

	valid := []byte("valid content")
	dig := digest.FromBytes(valid).String()
	r.NoError(archive.SaveBlob(t.Context(), inmemory.New(bytes.NewReader(valid))))

	corrupt := []byte("other content")
	r.Len(corrupt, len(valid))
	err = archive.SaveBlob(t.Context(), &mockBlob{
		digest: dig,
		known:  true,
		size:   int64(len(corrupt)),
		reader: failingCloseReader{bytes.NewReader(corrupt)},
	})
	r.ErrorContains(err, "digest mismatch")

	stored, err := archive.GetBlob(t.Context(), dig)
	r.NoError(err)
	rc, err := stored.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { _ = rc.Close() })
	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(valid, data)

	entries, err := os.ReadDir(filepath.Join(dir, ctf.BlobsDirectoryName))
	r.NoError(err)
	r.Len(entries, 1, "the staged file must be removed")
}

// lateDigestBlob reports its digest only once its content has been read to the end,
// like a blob that computes its digest while it is streamed. It records being asked
// for its digest before its reader was closed, which could make a buffering blob load
// its whole content into memory.
type lateDigestBlob struct {
	content    []byte
	closeErr   error
	digest     digest.Digest
	closed     bool
	askedEarly bool
}

func (b *lateDigestBlob) ReadCloser() (io.ReadCloser, error) {
	return &lateDigestReader{blob: b, r: bytes.NewReader(b.content)}, nil
}

func (b *lateDigestBlob) Digest() (string, bool) {
	if !b.closed {
		b.askedEarly = true
	}
	return b.digest.String(), b.digest != ""
}

type lateDigestReader struct {
	blob *lateDigestBlob
	r    *bytes.Reader
}

func (r *lateDigestReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err == io.EOF {
		r.blob.digest = digest.FromBytes(r.blob.content)
	}
	return n, err
}

func (r *lateDigestReader) Close() error {
	r.blob.closed = true
	return r.blob.closeErr
}

func TestFileSystemCTF_SaveBlob_DigestReportedAfterRead(t *testing.T) {
	content := []byte("streamed content")
	want := digest.FromBytes(content)
	wantFile := "sha256." + want.Encoded()

	tests := []struct {
		name        string
		preexisting bool
		closeErr    error
		wantErr     string
		wantFiles   []string
	}{
		{name: "stored under the reported digest", wantFiles: []string{wantFile}},
		{name: "already stored blob is kept", preexisting: true, wantFiles: []string{wantFile}},
		{name: "failed close stores nothing", closeErr: fmt.Errorf("digest mismatch"), wantErr: "digest mismatch"},
		{name: "failed close keeps stored blob", preexisting: true, closeErr: fmt.Errorf("digest mismatch"), wantErr: "digest mismatch", wantFiles: []string{wantFile}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			archive, err := ctf.OpenCTFFromOSPath(dir, ctf.O_RDWR)
			r.NoError(err)
			if tc.preexisting {
				r.NoError(archive.SaveBlob(t.Context(), inmemory.New(bytes.NewReader(content))))
			}

			b := &lateDigestBlob{content: content, closeErr: tc.closeErr}
			err = archive.SaveBlob(t.Context(), b)
			r.False(b.askedEarly, "the digest must only be asked for after the content was read and closed")
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
			} else {
				r.NoError(err)
			}

			entries, err := os.ReadDir(filepath.Join(dir, ctf.BlobsDirectoryName))
			r.NoError(err)
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			r.Equal(tc.wantFiles, names, "only the content-addressed blob may remain, no staged file")

			if tc.wantFiles == nil {
				return
			}
			stored, err := archive.GetBlob(t.Context(), want.String())
			r.NoError(err)
			rc, err := stored.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() { _ = rc.Close() })
			data, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(content, data)
		})
	}
}
