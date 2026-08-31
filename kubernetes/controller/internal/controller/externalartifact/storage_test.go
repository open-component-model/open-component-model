package externalartifact

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestStorageArchiveProducesDeterministicDigest(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "ocm.ocm-system.svc.cluster.local.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src1 := t.TempDir()
	writeFile(t, src1, "deployment.yaml", "kind: Deployment\n")
	writeFile(t, src1, "templates/svc.yaml", "kind: Service\n")

	res1, err := storage.Archive(context.Background(), "Resource", "apps", "podinfo", src1)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	if !strings.HasPrefix(res1.Digest, "sha256:") {
		t.Errorf("digest should be sha256-prefixed, got %q", res1.Digest)
	}
	if res1.Size <= 0 {
		t.Errorf("size should be positive, got %d", res1.Size)
	}
	wantPath := filepath.Join("resource", "apps", "podinfo", strings.TrimPrefix(res1.Digest, "sha256:")+".tar.gz")
	if res1.Path != wantPath {
		t.Errorf("path = %q, want %q", res1.Path, wantPath)
	}

	// Re-archiving identical content yields an identical digest (metadata is
	// normalised), which is what makes the revision stable across reconciles.
	src2 := t.TempDir()
	writeFile(t, src2, "deployment.yaml", "kind: Deployment\n")
	writeFile(t, src2, "templates/svc.yaml", "kind: Service\n")

	res2, err := storage.Archive(context.Background(), "Resource", "apps", "podinfo", src2)
	if err != nil {
		t.Fatalf("Archive (2): %v", err)
	}
	if res1.Digest != res2.Digest {
		t.Errorf("digest not deterministic: %q != %q", res1.Digest, res2.Digest)
	}
}

func TestStorageArtifactFields(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "ocm.ocm-system.svc.cluster.local.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src := t.TempDir()
	writeFile(t, src, "kustomization.yaml", "resources: []\n")

	res, err := storage.Archive(context.Background(), "Resource", "apps", "podinfo", src)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	artifact := storage.Artifact(res, "6.8.0")

	if artifact.Digest != res.Digest {
		t.Errorf("artifact digest = %q, want %q", artifact.Digest, res.Digest)
	}
	if artifact.Path != res.Path {
		t.Errorf("artifact path = %q, want %q", artifact.Path, res.Path)
	}
	if artifact.Size == nil || *artifact.Size != res.Size {
		t.Errorf("artifact size mismatch")
	}
	wantRevision := "6.8.0@" + res.Digest
	if artifact.Revision != wantRevision {
		t.Errorf("artifact revision = %q, want %q", artifact.Revision, wantRevision)
	}
	wantURL := "http://ocm.ocm-system.svc.cluster.local./" + filepath.ToSlash(res.Path)
	if artifact.URL != wantURL {
		t.Errorf("artifact url = %q, want %q", artifact.URL, wantURL)
	}
	if artifact.LastUpdateTime.IsZero() {
		t.Error("artifact lastUpdateTime should be set")
	}
}

func TestStorageArchiveContentRoundTrips(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src := t.TempDir()
	writeFile(t, src, "a.yaml", "content-a\n")
	writeFile(t, src, "nested/b.yaml", "content-b\n")

	res, err := storage.Archive(context.Background(), "Resource", "ns", "name", src)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}

	got := readTarGz(t, filepath.Join(base, res.Path))
	if got["a.yaml"] != "content-a\n" {
		t.Errorf("a.yaml = %q", got["a.yaml"])
	}
	if got["nested/b.yaml"] != "content-b\n" {
		t.Errorf("nested/b.yaml = %q", got["nested/b.yaml"])
	}
}

func TestStorageRemoveAllButCurrent(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	src1 := t.TempDir()
	writeFile(t, src1, "v.yaml", "v1\n")
	res1, err := storage.Archive(context.Background(), "Resource", "ns", "name", src1)
	if err != nil {
		t.Fatalf("Archive v1: %v", err)
	}

	src2 := t.TempDir()
	writeFile(t, src2, "v.yaml", "v2\n")
	res2, err := storage.Archive(context.Background(), "Resource", "ns", "name", src2)
	if err != nil {
		t.Fatalf("Archive v2: %v", err)
	}

	if err := storage.RemoveAllButCurrent("Resource", "ns", "name", res2.Path); err != nil {
		t.Fatalf("RemoveAllButCurrent: %v", err)
	}

	if _, err := os.Stat(filepath.Join(base, res1.Path)); !os.IsNotExist(err) {
		t.Errorf("stale artifact should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, res2.Path)); err != nil {
		t.Errorf("current artifact should remain, stat err = %v", err)
	}
}

func readTarGz(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer func() { _ = gzr.Close() }()

	out := map[string]string{}
	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		out[hdr.Name] = string(data)
	}

	return out
}
