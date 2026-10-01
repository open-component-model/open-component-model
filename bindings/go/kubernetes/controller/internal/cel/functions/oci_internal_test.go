package functions

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func componentInfoForTest(baseUrl, subPath string) *v1alpha1.ComponentInfo {
	repoSpec := fmt.Sprintf(`{"type":"OCIRepository/v1","baseUrl":"%s","subPath":"%s"}`, baseUrl, subPath)
	return &v1alpha1.ComponentInfo{
		RepositorySpec: &apiextensionsv1.JSON{Raw: []byte(repoSpec)},
		Component:      "my-component",
		Version:        "v1.0.0",
	}
}

func TestBuildImageReference(t *testing.T) {
	t.Parallel()

	t.Run("happy path builds correct reference", func(t *testing.T) {
		t.Parallel()
		blob := &v2.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			LocalReference: "sha256:abc123",
		}
		component := componentInfoForTest("https://ghcr.io", "myorg")

		ref, err := buildImageReference(blob, component)
		require.NoError(t, err)
		expected := "https://ghcr.io/myorg/component-descriptors/my-component@sha256:abc123"
		assert.Equal(t, expected, ref)
	})

	t.Run("empty baseUrl returns error", func(t *testing.T) {
		t.Parallel()
		blob := &v2.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			LocalReference: "sha256:abc123",
		}
		component := componentInfoForTest("", "")

		_, err := buildImageReference(blob, component)
		assert.Error(t, err, "expected error for empty base url")
	})

	t.Run("empty localReference returns error", func(t *testing.T) {
		t.Parallel()
		blob := &v2.LocalBlob{
			Type: runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
		}
		component := componentInfoForTest("https://ghcr.io", "")

		_, err := buildImageReference(blob, component)
		assert.Error(t, err, "expected error for local reference")
	})

	t.Run("nil component returns error", func(t *testing.T) {
		t.Parallel()
		blob := &v2.LocalBlob{
			Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			LocalReference: "sha256:abc123",
		}

		_, err := buildImageReference(blob, nil)
		assert.Error(t, err, "expected error for nil component")
	})
}
