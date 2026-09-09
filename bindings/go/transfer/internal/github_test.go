package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	githubv1 "ocm.software/open-component-model/bindings/go/github/spec/access/v1"
	githubv1alpha1 "ocm.software/open-component-model/bindings/go/github/transformation/spec/v1alpha1"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

func githubTestValue() *discoveryValue {
	return &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/test", Version: "1.0.0"},
				},
			},
		},
	}
}

func TestProcessGitHub_EmitsGetAndAdd(t *testing.T) {
	resource := descriptorv2.Resource{}
	resource.Name = "my-source"
	resource.Version = "1.0.0"
	resource.Type = "directoryTree"

	access := &githubv1.GitHub{
		Type:    runtime.NewVersionedType(githubv1.Type, githubv1.Version),
		RepoURL: "https://github.com/open-component-model/open-component-model",
		Commit:  "f58349914e3c775747dc1ee9af1bc83db4652266",
	}

	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	toSpec := testOCIRepo("ghcr.io/target")
	ids := map[int]string{}

	err := processGitHub(resource, access, "root", githubTestValue(), tgd, toSpec, ids, 0)
	require.NoError(t, err)

	require.Len(t, tgd.Transformations, 2)
	assert.Equal(t, githubv1alpha1.GetGitHubCommitV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, tgd.Transformations[1].Type)

	getID := tgd.Transformations[0].ID
	assert.Equal(t, "${"+getID+".output.file}", tgd.Transformations[1].Spec.Data["file"])
	assert.Equal(t, tgd.Transformations[1].ID, ids[0])

	// referenceName must stay an OCI repository name, so it is the resource name.
	addedResource, ok := tgd.Transformations[1].Spec.Data["resource"].(map[string]any)
	require.True(t, ok)
	addedAccess, ok := addedResource["access"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "my-source", addedAccess["referenceName"])
}

// A ref-only access would embed whatever the ref points at today, so it is refused.
func TestProcessGitHub_RejectsUnpinnedAccess(t *testing.T) {
	resource := descriptorv2.Resource{}
	resource.Name = "my-source"
	resource.Version = "1.0.0"
	resource.Type = "directoryTree"

	access := &githubv1.GitHub{
		Type:    runtime.NewVersionedType(githubv1.Type, githubv1.Version),
		RepoURL: "https://github.com/open-component-model/open-component-model",
		Ref:     "refs/heads/main",
	}

	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	err := processGitHub(resource, access, "root", githubTestValue(), tgd, testOCIRepo("ghcr.io/target"), map[int]string{}, 0)
	require.Error(t, err)
	assert.ErrorContains(t, err, "no pinned commit")
	assert.Empty(t, tgd.Transformations, "a rejected resource must not add transformation nodes")
}
