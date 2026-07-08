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
