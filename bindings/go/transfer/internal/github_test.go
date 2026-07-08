package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	githubv1alpha1 "ocm.software/open-component-model/bindings/go/github/transformation/spec/v1alpha1"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

func TestProcessGitHub_EmitsGetAndAdd(t *testing.T) {
	resource := descriptorv2.Resource{}
	resource.Name = "my-source"
	resource.Version = "1.0.0"
	resource.Type = "gitHub"
	resource.Access = &runtime.Raw{Data: []byte("{}")} // access content is irrelevant to graph shape

	val := &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/test", Version: "1.0.0"},
				},
			},
		},
	}
	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	toSpec := testOCIRepo("ghcr.io/target")
	ids := map[int]string{}

	err := processGitHub(resource, "root", val, tgd, toSpec, ids, 0)
	require.NoError(t, err)

	require.Len(t, tgd.Transformations, 2)
	assert.Equal(t, githubv1alpha1.GetGitHubCommitV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, tgd.Transformations[1].Type)

	// the add node consumes the buffered tarball from the get node's contentFile output
	getID := tgd.Transformations[0].ID
	assert.Equal(t, "${"+getID+".output.contentFile}", tgd.Transformations[1].Spec.Data["file"])
	assert.Equal(t, tgd.Transformations[1].ID, ids[0])
}
