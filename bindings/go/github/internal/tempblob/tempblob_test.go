package tempblob

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mediaType = "application/x-tgz"

// filesIn lists the entries of dir, so tests can assert on temp file lifetime.
func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func readAll(t *testing.T, b *Blob) string {
	t.Helper()
	r, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, r.Close()) }()
	data, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(data)
}

func TestNew_StreamsContentToFileUnderDir(t *testing.T) {
	dir := t.TempDir()

	b, err := New(dir, "github-archive-*.tgz", strings.NewReader("hello world"), mediaType)
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	names := filesIn(t, dir)
	require.Len(t, names, 1, "the archive must be buffered as a file under the temp dir")
	assert.True(t, strings.HasSuffix(names[0], ".tgz"), "got %q", names[0])

	assert.Equal(t, "hello world", readAll(t, b))
}

func TestNew_ReportsSizeAndMediaType(t *testing.T) {
	b, err := New(t.TempDir(), "blob-*", strings.NewReader("hello world"), mediaType)
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	assert.Equal(t, int64(len("hello world")), b.Size())

	mt, known := b.MediaType()
	assert.True(t, known)
	assert.Equal(t, mediaType, mt)
}

// The blob.ReadOnlyBlob contract requires ReadCloser to be callable multiple
// times, each invocation returning a reader that starts from the beginning.
func TestReadCloser_IsRepeatableFromTheStart(t *testing.T) {
	b, err := New(t.TempDir(), "blob-*", strings.NewReader("hello world"), mediaType)
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	assert.Equal(t, "hello world", readAll(t, b))
	assert.Equal(t, "hello world", readAll(t, b), "a second reader must start from the beginning")
}

func TestClose_RemovesTheFileAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	b, err := New(dir, "blob-*", strings.NewReader("hello world"), mediaType)
	require.NoError(t, err)
	require.Len(t, filesIn(t, dir), 1)

	require.NoError(t, b.Close())
	assert.Empty(t, filesIn(t, dir), "Close must remove the buffered file")

	require.NoError(t, b.Close(), "Close must be idempotent")
}

// A Close that cannot remove the file must report the error and leave the blob
// open: the file is still there, so ReadCloser must keep working and a later
// Close must be able to retry. Marking the blob closed regardless would strand
// a readable file behind a blob that refuses to hand out readers.
func TestClose_FailureLeavesBlobOpen(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory permissions, so os.Remove cannot be made to fail this way")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "sub")
	require.NoError(t, os.MkdirAll(dir, 0o700))

	b, err := New(dir, "blob-*", strings.NewReader("hello world"), mediaType)
	require.NoError(t, err)

	// Make the parent directory unwritable so os.Remove(b.path) fails.
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	require.Error(t, b.Close(), "Close must report that it could not remove the file")

	assert.Equal(t, "hello world", readAll(t, b), "ReadCloser must keep working after a failed Close")

	// Once removal is possible again, Close succeeds and the file is gone.
	require.NoError(t, os.Chmod(dir, 0o700))
	require.NoError(t, b.Close(), "a retried Close must succeed once removal is possible")
	assert.Empty(t, filesIn(t, dir))
}

// Close removes the file and cancels the cleanup. Were the cleanup left armed,
// it would fire once the blob became unreachable and remove whatever file then
// occupied the name — os.CreateTemp is free to hand the same name out again.
func TestClose_CancelsTheCleanup(t *testing.T) {
	dir := t.TempDir()

	path := func() string {
		b, err := New(dir, "blob-*", strings.NewReader("hello world"), mediaType)
		require.NoError(t, err)
		require.NoError(t, b.Close())
		return b.path // b is unreachable past this point, arming any live cleanup
	}()

	// Stand in for a later blob that happens to reuse the name.
	require.NoError(t, os.WriteFile(path, []byte("someone else's bytes"), 0o600))

	for range 10 {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err, "a closed blob's cleanup must not remove a file that later reuses its name")
	assert.Equal(t, "someone else's bytes", string(data))
}

func TestReadCloser_FailsAfterClose(t *testing.T) {
	b, err := New(t.TempDir(), "blob-*", strings.NewReader("hello world"), mediaType)
	require.NoError(t, err)
	require.NoError(t, b.Close())

	_, err = b.ReadCloser()
	assert.Error(t, err, "a closed blob must not hand out readers")
}

// No DownloadResource caller closes the returned blob today, so an unclosed
// blob must not leak its file. Once the blob is unreachable, the file goes.
func TestUnclosedBlob_FileIsRemovedOnceUnreachable(t *testing.T) {
	dir := t.TempDir()

	func() {
		b, err := New(dir, "blob-*", strings.NewReader("hello world"), mediaType)
		require.NoError(t, err)
		require.Len(t, filesIn(t, dir), 1)
		_ = b // b goes out of scope here without Close
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if len(filesIn(t, dir)) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("unclosed blob leaked its file in %s: %v", filepath.Base(dir), filesIn(t, dir))
}
