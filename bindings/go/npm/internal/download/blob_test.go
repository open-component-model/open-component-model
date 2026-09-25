package download

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	accessv1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
)

func downloadTestBlob(t *testing.T, tempDir string) *Blob {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "payload")
	}))
	t.Cleanup(srv.Close)
	b, err := downloadTarball(t.Context(), &accessv1.NPM{Registry: srv.URL},
		Version{Dist: Dist{Tarball: srv.URL}}, nil, Options{TempDir: tempDir})
	require.NoError(t, err)
	return b
}

func TestBlobCloseRemovesTempFile(t *testing.T) {
	tempDir := t.TempDir()
	b := downloadTestBlob(t, tempDir)
	entries, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.NoError(t, b.Close())
	entries, err = os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Empty(t, entries)
	require.NoError(t, b.Close(), "closing twice must not fail")
	_, err = b.ReadCloser()
	require.Error(t, err)
}

func requireReclaimedBlob(t *testing.T, tempDir string) {
	t.Helper()
	// Cleanup runs asynchronously after the blob becomes unreachable.
	require.Eventually(t, func() bool {
		runtime.GC()
		entries, err := os.ReadDir(tempDir)
		return err == nil && len(entries) == 0
	}, 10*time.Second, 20*time.Millisecond, "an abandoned blob must not leave its temporary file behind")
}

func TestBlobAbandonedIsReclaimed(t *testing.T) {
	tempDir := t.TempDir()
	func() {
		b := downloadTestBlob(t, tempDir)
		entries, err := os.ReadDir(tempDir)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		runtime.KeepAlive(b)
	}()
	requireReclaimedBlob(t, tempDir)
}

func TestBlobReaderRetainsOwner(t *testing.T) {
	tempDir := t.TempDir()
	func() {
		rc := func() io.ReadCloser {
			b := downloadTestBlob(t, tempDir)
			rc, err := b.ReadCloser()
			require.NoError(t, err)
			return rc
		}()
		defer func() { require.NoError(t, rc.Close()) }()

		require.Never(t, func() bool {
			runtime.GC()
			entries, err := os.ReadDir(tempDir)
			return err != nil || len(entries) != 1
		}, 200*time.Millisecond, 20*time.Millisecond, "an open reader must retain its blob")

		data, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, "payload", string(data))
	}()
	requireReclaimedBlob(t, tempDir)
}
