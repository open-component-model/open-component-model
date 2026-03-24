package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestStaticReferenceName_EmptyBaseURL(t *testing.T) {
	fn := staticReferenceName("my/image:v1.0.0")
	assert.Equal(t, "my/image:v1.0.0", fn(""))
}

func TestStaticReferenceName_WithBaseURL(t *testing.T) {
	fn := staticReferenceName("my/image:v1.0.0")
	assert.Equal(t, "ghcr.io/org/my/image:v1.0.0", fn("ghcr.io/org"))
}

func TestImageReferenceFromAccess_EmptyBaseURL(t *testing.T) {
	fn := imageReferenceFromAccess("getStep1")
	result := fn("")
	assert.Equal(t, "${getStep1.output.resource.access.imageReference}", result)
}

func TestImageReferenceFromAccess_WithBaseURL(t *testing.T) {
	fn := imageReferenceFromAccess("getStep1")
	result := fn("ghcr.io/org")
	assert.Equal(t, "ghcr.io/org/${getStep1.output.resource.access.imageReference}", result)
}

func TestOciUploadAsLocalResource_OCI(t *testing.T) {
	toSpec := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
	}

	transform, err := ociUploadAsLocalResource(toSpec, "comp", "1.0.0", "addRes1", "getRes1", staticReferenceName("my/image:v1"))
	require.NoError(t, err)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, transform.TransformationMeta.Type)
	assert.Equal(t, "addRes1", transform.TransformationMeta.ID)
	assert.NotNil(t, transform.Spec)
}

func TestOciUploadAsArtifact_WithSubPath(t *testing.T) {
	toSpec := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
		SubPath: "my-org/components",
	}

	transform, err := ociUploadAsArtifact(toSpec, "addRes1", "getRes1", staticReferenceName("my/image:v1"))
	require.NoError(t, err)

	spec := transform.Spec
	resource := spec.Data["resource"].(map[string]any)
	access := resource["access"].(map[string]interface{})
	assert.Contains(t, access["imageReference"], "ghcr.io/my-org/components/my/image:v1")
}

func TestOciUploadAsArtifact_NoSubPath(t *testing.T) {
	toSpec := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
	}

	transform, err := ociUploadAsArtifact(toSpec, "addRes1", "getRes1", staticReferenceName("my/image:v1"))
	require.NoError(t, err)

	spec := transform.Spec
	resource := spec.Data["resource"].(map[string]any)
	access := resource["access"].(map[string]interface{})
	assert.Equal(t, "ghcr.io/my/image:v1", access["imageReference"])
}
