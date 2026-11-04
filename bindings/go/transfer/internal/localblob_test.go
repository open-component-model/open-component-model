package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func localBlobTestRepo() *oci.Repository {
	return &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
	}
}

func TestUploadAsLocalResource_OCI(t *testing.T) {
	transform, err := uploadAsLocalResource(localBlobTestRepo(), "comp", "1.0.0", "addRes1", "getRes1", "my/image:v1", "comp@1.0.0 [Add my-image]")
	require.NoError(t, err)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, transform.Type)
	assert.Equal(t, "addRes1", transform.ID)
	assert.Equal(t, "comp@1.0.0 [Add my-image]", transform.Label)
	require.NotNil(t, transform.Spec)

	// A non-empty referenceName is written to the local blob access (OCI legacy compat).
	access := transform.Spec.Data["resource"].(map[string]any)["access"].(map[string]any)
	assert.Equal(t, "my/image:v1", access["referenceName"])
}

// referenceName is an OCI-only legacy field. When no reference name is supplied (every
// non-OCI content type: git, github, s3, wget), the generated local blob access must omit
// the field entirely rather than carrying an empty one.
func TestUploadAsLocalResource_OmitsEmptyReferenceName(t *testing.T) {
	transform, err := uploadAsLocalResource(localBlobTestRepo(), "comp", "1.0.0", "addRes1", "getRes1", "", "comp@1.0.0 [Add blob]")
	require.NoError(t, err)

	access := transform.Spec.Data["resource"].(map[string]any)["access"].(map[string]any)
	_, hasReferenceName := access["referenceName"]
	assert.False(t, hasReferenceName)
}
