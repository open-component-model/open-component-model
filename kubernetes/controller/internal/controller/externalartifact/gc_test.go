package externalartifact

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

// testScheme registers the OCM and Flux source types used across the package's
// fake-client tests.
func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add ocm scheme: %v", err)
	}
	if err := sourcev1.AddToScheme(s); err != nil {
		t.Fatalf("add flux scheme: %v", err)
	}

	return s
}

// seedArtifact packages a trivial artifact for the given object so the GC has a
// directory to consider.
func seedArtifact(t *testing.T, storage *Storage, namespace, name string) {
	t.Helper()
	src := stage(t, map[string]string{"a.yaml": "kind: ConfigMap\n"})
	if _, err := storage.Archive(context.Background(), v1alpha1.KindResource, namespace, name, src); err != nil {
		t.Fatalf("seed archive: %v", err)
	}
}

func artifactDirExists(base, namespace, name string) bool {
	_, err := os.Stat(filepath.Join(base, ArtifactDir(v1alpha1.KindResource, namespace, name)))

	return err == nil
}

func TestGarbageCollectorRemovesOrphansKeepsLive(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	// Two artifacts on disk: one whose Resource still exists, one orphaned.
	seedArtifact(t, storage, "ns", "live")
	seedArtifact(t, storage, "ns", "orphan")

	liveResource := &v1alpha1.Resource{
		ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "live"},
	}
	c := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(liveResource).Build()

	gc := NewGarbageCollector(c, storage, time.Hour, nil)
	if err := gc.sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if !artifactDirExists(base, "ns", "live") {
		t.Error("artifact for live Resource must be kept")
	}
	if artifactDirExists(base, "ns", "orphan") {
		t.Error("artifact for deleted Resource must be removed")
	}
}

func TestGarbageCollectorIgnoresUnknownKinds(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	// A directory under an unmanaged kind must never be touched.
	otherDir := filepath.Join(base, "somethingelse", "ns", "keep")
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	gc := NewGarbageCollector(c, storage, time.Hour, nil)
	if err := gc.sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}

	if _, err := os.Stat(otherDir); err != nil {
		t.Errorf("unmanaged-kind directory must be left untouched: %v", err)
	}
}

func TestGarbageCollectorEmptyStore(t *testing.T) {
	base := t.TempDir()
	storage, err := NewStorage(base, "example.svc.")
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}

	c := fake.NewClientBuilder().WithScheme(testScheme(t)).Build()
	gc := NewGarbageCollector(c, storage, time.Hour, nil)
	if err := gc.sweep(context.Background()); err != nil {
		t.Fatalf("sweep on empty store: %v", err)
	}
}
