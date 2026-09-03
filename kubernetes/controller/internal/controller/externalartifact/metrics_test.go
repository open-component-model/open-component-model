package externalartifact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// freshMetrics builds a Metrics wired to a private registry so assertions are
// isolated from the process-global collectors.
func freshMetrics(t *testing.T) *Metrics {
	t.Helper()
	reg := prometheus.NewRegistry()

	return newMetricsFor(reg)
}

func TestArchiveRecordsMetrics(t *testing.T) {
	m := freshMetrics(t)
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.", WithMetrics(m))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src := stage(t, map[string]string{"a.yaml": "kind: ConfigMap\n"})
	if _, err := storage.Archive(context.Background(), "Resource", "ns", "name", src); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if got := testutil.ToFloat64(m.ArtifactsProducedTotal); got != 1 {
		t.Errorf("ArtifactsProducedTotal = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.ArchiveErrorsTotal); got != 0 {
		t.Errorf("ArchiveErrorsTotal = %v, want 0", got)
	}
	if got := testutil.CollectAndCount(m.ArchiveDurationSeconds); got != 1 {
		t.Errorf("ArchiveDurationSeconds sample count = %v, want 1", got)
	}
}

func TestArchiveErrorRecordsMetric(t *testing.T) {
	m := freshMetrics(t)
	base := t.TempDir()
	// Tiny cap forces a packaging failure.
	storage, err := NewStorage(base, "example.svc.", WithMetrics(m), WithMaxArtifactSize(4))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src := stage(t, map[string]string{"big.yaml": strings.Repeat("x", 8192)})
	if _, err := storage.Archive(context.Background(), "Resource", "ns", "name", src); err == nil {
		t.Fatal("expected Archive to fail")
	}

	if got := testutil.ToFloat64(m.ArchiveErrorsTotal); got != 1 {
		t.Errorf("ArchiveErrorsTotal = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.ArtifactsProducedTotal); got != 0 {
		t.Errorf("ArtifactsProducedTotal = %v, want 0", got)
	}
}

func TestServeRecordsMetrics(t *testing.T) {
	m := freshMetrics(t)
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.", WithMetrics(m))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	src := stage(t, map[string]string{"a.yaml": "hi\n"})
	res, err := storage.Archive(context.Background(), "Resource", "ns", "name", src)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	// A 2xx (file) and a 4xx (missing) request.
	serveOnce(t, storage, "/"+res.Path)
	serveOnce(t, storage, "/resource/ns/name/missing.tar.gz")

	if got := testutil.ToFloat64(m.ServeRequestsTotal.WithLabelValues("2xx")); got != 1 {
		t.Errorf("serve 2xx = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.ServeRequestsTotal.WithLabelValues("4xx")); got != 1 {
		t.Errorf("serve 4xx = %v, want 1", got)
	}
}

func TestVerifyIntegrityIsIncremental(t *testing.T) {
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

	// First pass verifies (and caches) the artifact.
	if n, err := storage.VerifyIntegrity(); err != nil || n != 0 {
		t.Fatalf("first VerifyIntegrity = (%d,%v), want (0,nil)", n, err)
	}
	// The fingerprint must now be cached for the artifact's relative path.
	if _, ok := storage.verifiedCache.Load(res.Path); !ok {
		t.Fatal("expected artifact fingerprint to be cached after clean verification")
	}

	// Second pass: unchanged file is skipped (still clean, still cached).
	if n, err := storage.VerifyIntegrity(); err != nil || n != 0 {
		t.Fatalf("second VerifyIntegrity = (%d,%v), want (0,nil)", n, err)
	}

	// Corrupt the file: the incremental skip must NOT hide it, because the size
	// changes (fingerprint mismatch) → re-hash → removal.
	if err := os.WriteFile(filepath.Join(base, res.Path), []byte("corrupt"), 0o600); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if n, err := storage.VerifyIntegrity(); err != nil || n != 1 {
		t.Fatalf("VerifyIntegrity after corruption = (%d,%v), want (1,nil)", n, err)
	}
	if _, ok := storage.verifiedCache.Load(res.Path); ok {
		t.Error("cache entry must be dropped for a removed corrupt artifact")
	}
}

func serveOnce(t *testing.T, storage *Storage, path string) {
	t.Helper()
	srv := httptest.NewServer(storage.Server())
	defer srv.Close()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	_ = resp.Body.Close()
}
