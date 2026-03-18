package functions

import (
	"testing"

	"github.com/stretchr/testify/assert"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
)

func TestBuildImageReference(t *testing.T) {
	t.Parallel()

	t.Run("happy path builds correct reference", func(t *testing.T) {
		t.Parallel()
		repo := &oci.Repository{
			Type:    runtime.NewVersionedType("OCIRepository", "v1"),
			BaseUrl: "https://ghcr.io",
			SubPath: "myorg",
		}
		blob := &v2.LocalBlob{
			LocalReference: "sha256:abc123",
		}
		component := &v1alpha1.ComponentInfo{
			Component: "my-component",
		}

		ref, err := buildImageReference(repo, blob, component)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := "https://ghcr.io/myorg/component-descriptors/my-component@sha256:abc123"
		assert.Equal(t, expected, ref)
	})

	t.Run("empty base url returns error", func(t *testing.T) {
		t.Parallel()
		repo := &oci.Repository{
			Type: runtime.NewVersionedType("OCIRepository", "v1"),
		}
		blob := &v2.LocalBlob{
			LocalReference: "sha256:abc123",
		}
		component := &v1alpha1.ComponentInfo{
			Component: "my-component",
		}

		_, err := buildImageReference(repo, blob, component)
		assert.Errorf(t, err, "expected error for empty base url")
	})

	t.Run("empty local reference returns error", func(t *testing.T) {
		t.Parallel()
		repo := &oci.Repository{
			Type:    runtime.NewVersionedType("OCIRepository", "v1"),
			BaseUrl: "https://ghcr.io",
		}
		blob := &v2.LocalBlob{}
		component := &v1alpha1.ComponentInfo{
			Component: "my-component",
		}

		_, err := buildImageReference(repo, blob, component)
		assert.Errorf(t, err, "expected error for local reference")
	})

	t.Run("nil component returns error", func(t *testing.T) {
		t.Parallel()
		repo := &oci.Repository{
			Type:    runtime.NewVersionedType("OCIRepository", "v1"),
			BaseUrl: "https://ghcr.io",
		}
		blob := &v2.LocalBlob{
			LocalReference: "sha256:abc123",
		}

		_, err := buildImageReference(repo, blob, nil)
		assert.Errorf(t, err, "expected error for nil component")
	})
}
