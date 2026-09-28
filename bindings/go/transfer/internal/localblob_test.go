package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestUploadAsLocalResource_OCI(t *testing.T) {
	toSpec := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
	}

	transform, err := uploadAsLocalResource(toSpec, "comp", "1.0.0", "addRes1", "getRes1", staticReferenceName("my/image:v1"), "comp@1.0.0 [Add my-image]")
	require.NoError(t, err)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, transform.Type)
	assert.Equal(t, "addRes1", transform.ID)
	assert.Equal(t, "comp@1.0.0 [Add my-image]", transform.Label)
	assert.NotNil(t, transform.Spec)
}
