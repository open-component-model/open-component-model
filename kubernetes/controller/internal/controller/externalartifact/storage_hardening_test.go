package externalartifact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// stage writes a small content tree into a fresh temp dir and returns it.
func stage(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	return dir
}

func TestStorageArchiveEnforcesMaxSize(t *testing.T) {
	base := t.TempDir()
	// Tiny cap so even a small file exceeds it after gzip framing.
	storage, err := NewStorage(base, "example.svc.", WithMaxArtifactSize(16))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src := stage(t, map[string]string{"big.yaml": strings.Repeat("x", 4096)})

	if _, err := storage.Archive(context.Background(), "Resource", "ns", "name", src); err == nil {
		t.Fatal("expected Archive to fail on oversized content, got nil")
	}
}

func TestStorageArchiveUnlimitedByDefault(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.") // no cap
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src := stage(t, map[string]string{"big.yaml": strings.Repeat("x", 100_000)})
	if _, err := storage.Archive(context.Background(), "Resource", "ns", "name", src); err != nil {
		t.Fatalf("Archive should succeed with no cap: %v", err)
	}
}

func TestStorageVerifyDetectsWipeAndCorruption(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src := stage(t, map[string]string{"a.yaml": "hello\n"})
	res, err := storage.Archive(context.Background(), "Resource", "ns", "name", src)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Intact artifact verifies.
	ok, err := storage.Verify(res.Path, res.Digest)
	if err != nil || !ok {
		t.Fatalf("Verify intact = (%v, %v), want (true, nil)", ok, err)
	}

	// Exists is true.
	if !storage.Exists(res.Path) {
		t.Fatal("Exists should be true for a present artifact")
	}

	// Wiped store: file gone → Verify false, no error (caller repackages).
	if err := os.Remove(filepath.Join(base, res.Path)); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if ok, err := storage.Verify(res.Path, res.Digest); err != nil || ok {
		t.Fatalf("Verify after wipe = (%v, %v), want (false, nil)", ok, err)
	}
	if storage.Exists(res.Path) {
		t.Fatal("Exists should be false after wipe")
	}

	// Corrupted content: digest mismatch → Verify false.
	corrupt := stage(t, map[string]string{"a.yaml": "tampered\n"})
	res2, err := storage.Archive(context.Background(), "Resource", "ns", "name", corrupt)
	if err != nil {
		t.Fatalf("Archive corrupt: %v", err)
	}
	if ok, err := storage.Verify(res2.Path, res.Digest); err != nil || ok {
		t.Fatalf("Verify with wrong digest = (%v, %v), want (false, nil)", ok, err)
	}
}

func TestStorageServerDisablesDirectoryListing(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	src := stage(t, map[string]string{"a.yaml": "hi\n"})
	res, err := storage.Archive(context.Background(), "Resource", "ns", "name", src)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	srv := httptest.NewServer(storage.Server())
	defer srv.Close()

	// The artifact file itself is served.
	fileResp, err := http.Get(srv.URL + "/" + res.Path)
	if err != nil {
		t.Fatalf("GET file: %v", err)
	}
	_ = fileResp.Body.Close()
	if fileResp.StatusCode != http.StatusOK {
		t.Errorf("GET artifact = %d, want 200", fileResp.StatusCode)
	}

	// A directory request must not list contents (404).
	dirResp, err := http.Get(srv.URL + "/resource/ns/name/")
	if err != nil {
		t.Fatalf("GET dir: %v", err)
	}
	_ = dirResp.Body.Close()
	if dirResp.StatusCode != http.StatusNotFound {
		t.Errorf("GET directory = %d, want 404 (no listing)", dirResp.StatusCode)
	}

	// Non-GET/HEAD is rejected.
	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/"+res.Path, nil)
	delResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	_ = delResp.Body.Close()
	if delResp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("DELETE artifact = %d, want 405", delResp.StatusCode)
	}
}

func TestStorageArchiveConcurrentDistinctKeys(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	const n = 8
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			src := stage(t, map[string]string{"a.yaml": strings.Repeat("y", i+1)})
			_, err := storage.Archive(context.Background(), "Resource", "ns", "name-"+string(rune('a'+i)), src)
			errCh <- err
		}(i)
	}
	wg.Wait()
	close(errCh)

	for e := range errCh {
		if e != nil {
			t.Errorf("concurrent Archive failed: %v", e)
		}
	}
}

func TestStorageDiskUsageAndListObjects(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	// Empty store: zero usage, no objects.
	if b, c, err := storage.DiskUsage(); err != nil || b != 0 || c != 0 {
		t.Fatalf("empty DiskUsage = (%d,%d,%v), want (0,0,nil)", b, c, err)
	}
	if objs, err := storage.ListArtifactObjects(); err != nil || len(objs) != 0 {
		t.Fatalf("empty ListArtifactObjects = (%v,%v), want (0,nil)", objs, err)
	}

	seedArtifact(t, storage, "ns1", "a")
	seedArtifact(t, storage, "ns2", "b")

	bytes, count, err := storage.DiskUsage()
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}
	if count != 2 {
		t.Errorf("file count = %d, want 2", count)
	}
	if bytes <= 0 {
		t.Errorf("total bytes = %d, want > 0", bytes)
	}

	objs, err := storage.ListArtifactObjects()
	if err != nil {
		t.Fatalf("ListArtifactObjects: %v", err)
	}
	if len(objs) != 2 {
		t.Errorf("object count = %d, want 2", len(objs))
	}
	for _, o := range objs {
		if o.Kind != "resource" {
			t.Errorf("object kind = %q, want resource", o.Kind)
		}
	}
}

func TestStorageVerifyIntegrityRemovesCorrupt(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src := stage(t, map[string]string{"a.yaml": "kind: ConfigMap\n"})
	res, err := storage.Archive(context.Background(), "Resource", "ns", "name", src)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// Intact store: nothing removed.
	if n, err := storage.VerifyIntegrity(); err != nil || n != 0 {
		t.Fatalf("VerifyIntegrity intact = (%d,%v), want (0,nil)", n, err)
	}

	// Corrupt the artifact content in place (keep the digest-named filename).
	full := filepath.Join(base, res.Path)
	if err := os.WriteFile(full, []byte("corrupted-bytes"), 0o600); err != nil {
		t.Fatalf("corrupt: %v", err)
	}

	n, err := storage.VerifyIntegrity()
	if err != nil {
		t.Fatalf("VerifyIntegrity: %v", err)
	}
	if n != 1 {
		t.Errorf("removed = %d, want 1", n)
	}
	if _, err := os.Stat(full); !os.IsNotExist(err) {
		t.Errorf("corrupt artifact should have been removed, stat err = %v", err)
	}
}

func TestStorageSizeMatches(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src := stage(t, map[string]string{"a.yaml": "kind: ConfigMap\n"})
	res, err := storage.Archive(context.Background(), "Resource", "ns", "name", src)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if !storage.SizeMatches(res.Path, res.Size) {
		t.Error("SizeMatches should be true for the correct size")
	}
	if storage.SizeMatches(res.Path, res.Size+1) {
		t.Error("SizeMatches should be false for a wrong size")
	}
	if storage.SizeMatches(res.Path, 0) {
		t.Error("SizeMatches should be false for a non-positive size")
	}
	if storage.SizeMatches("resource/ns/name/does-not-exist.tar.gz", 10) {
		t.Error("SizeMatches should be false for a missing file")
	}
}
