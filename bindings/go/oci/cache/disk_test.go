package cache

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteAtomic_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	data := []byte("hello world")
	dgst := digest.FromBytes(data)

	path, n, err := writeAtomic(dir, dgst, 0, bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)), n)
	assert.Equal(t, pathFor(dir, dgst), path)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestWriteAtomic_NoTempLeak_OnSuccess(t *testing.T) {
	dir := t.TempDir()
	data := []byte("ok")
	dgst := digest.FromBytes(data)

	_, _, err := writeAtomic(dir, dgst, 0, bytes.NewReader(data))
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".incoming-"),
			"leftover temp file %s", e.Name())
	}
}

type erroringReader struct{ after int }

func (r *erroringReader) Read(p []byte) (int, error) {
	if r.after <= 0 {
		return 0, errors.New("boom")
	}
	n := r.after
	if n > len(p) {
		n = len(p)
	}
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	r.after -= n
	return n, nil
}

func TestWriteAtomic_NoTempLeak_OnError(t *testing.T) {
	dir := t.TempDir()
	dgst := digest.FromBytes([]byte("anything"))

	_, _, err := writeAtomic(dir, dgst, 0, &erroringReader{after: 4})
	require.Error(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".incoming-"),
			"leftover temp file %s", e.Name())
	}
	_, err = os.Stat(pathFor(dir, dgst))
	assert.True(t, errors.Is(err, fs.ErrNotExist), "final file must not exist on error")
}

func TestWriteAtomic_SizeCap(t *testing.T) {
	dir := t.TempDir()
	data := []byte("0123456789")
	dgst := digest.FromBytes(data)

	_, _, err := writeAtomic(dir, dgst, 4, bytes.NewReader(data))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds max size")

	// Final file must not exist.
	_, err = os.Stat(pathFor(dir, dgst))
	assert.True(t, errors.Is(err, fs.ErrNotExist))

	// And no temp file left behind.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, strings.HasPrefix(e.Name(), ".incoming-"))
	}
}

func TestWriteAtomic_InvalidDigest(t *testing.T) {
	dir := t.TempDir()
	_, _, err := writeAtomic(dir, digest.Digest("not-a-digest"), 0, bytes.NewReader(nil))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid digest")
}

func TestEnsureDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "deep")
	require.NoError(t, ensureDir(dir))
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Idempotent.
	require.NoError(t, ensureDir(dir))
}

func TestRemoveQuiet(t *testing.T) {
	// missing path: no error
	assert.NoError(t, removeQuiet(filepath.Join(t.TempDir(), "missing")))

	// existing path: removed
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	require.NoError(t, os.WriteFile(p, []byte("x"), 0o600))
	require.NoError(t, removeQuiet(p))
	_, err := os.Stat(p)
	assert.True(t, errors.Is(err, fs.ErrNotExist))
}

// helper used in store_test.go too
var _ io.Reader = (*erroringReader)(nil)

func TestRemoveQuiet_NotFoundIsNotAnError(t *testing.T) {
	require.NoError(t, removeQuiet(filepath.Join(t.TempDir(), "missing")))
}

func TestRemoveQuiet_PropagatesOtherErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dir-remove semantics differ on windows")
	}
	dir := t.TempDir()
	// Removing a non-empty directory returns an error other than ErrNotExist.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "child"), nil, 0o600))
	err := removeQuiet(dir)
	require.Error(t, err)
}

func TestEnsureDir_FailsOnFileInPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on windows")
	}
	parent := t.TempDir()
	blocker := filepath.Join(parent, "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	err := ensureDir(filepath.Join(blocker, "x"))
	require.Error(t, err)
}

func TestScanExisting_RemovesIncomingAndBadFiles(t *testing.T) {
	dir := t.TempDir()
	algoDir := filepath.Join(dir, "sha256")
	require.NoError(t, os.MkdirAll(algoDir, 0o700))

	stale := filepath.Join(algoDir, ".incoming-stale")
	bad := filepath.Join(algoDir, "not-a-hex")
	require.NoError(t, os.WriteFile(stale, []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(bad, []byte("y"), 0o600))

	good := digest.FromBytes([]byte("ok"))
	goodPath := pathFor(dir, good)
	require.NoError(t, os.WriteFile(goodPath, []byte("ok"), 0o600))

	entries, err := scanExisting(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, good, entries[0].digest)

	for _, p := range []string{stale, bad} {
		_, err := os.Stat(p)
		assert.ErrorIs(t, err, fs.ErrNotExist, "stray %s must be removed", p)
	}
}

func TestScanExisting_MissingDirIsNoOp(t *testing.T) {
	entries, err := scanExisting(filepath.Join(t.TempDir(), "missing"))
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestScanExisting_AlgoReadDirFails(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("permission bits do not block root or windows")
	}
	dir := t.TempDir()
	algo := filepath.Join(dir, "sha256")
	require.NoError(t, os.MkdirAll(algo, 0o700))
	require.NoError(t, os.Chmod(algo, 0))
	t.Cleanup(func() { _ = os.Chmod(algo, 0o700) })

	_, err := scanExisting(dir)
	require.Error(t, err)
}
