package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	mavenaccess "ocm.software/open-component-model/bindings/go/maven/spec/access"
	mavenv2alpha1 "ocm.software/open-component-model/bindings/go/maven/spec/access/v2alpha1"
	mavenv1alpha1 "ocm.software/open-component-model/bindings/go/maven/transformation/spec/v1alpha1"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

func mavenTestValue() *discoveryValue {
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

// rawAccess encodes access the way a v2 descriptor carries it.
func rawAccess(t *testing.T, access *mavenv2alpha1.Maven) *runtime.Raw {
	t.Helper()
	raw := &runtime.Raw{}
	require.NoError(t, mavenaccess.Scheme.Convert(access, raw))
	return raw
}

func mavenTestAccess(version string) *mavenv2alpha1.Maven {
	return &mavenv2alpha1.Maven{
		Type:       runtime.NewVersionedType(mavenv2alpha1.Type, mavenv2alpha1.Version),
		RepoURL:    "https://repo1.maven.org/maven2",
		GroupID:    "org.springframework.kafka",
		ArtifactID: "spring-kafka",
		Version:    version,
		Artifacts:  []mavenv2alpha1.Artifact{{Extension: "jar"}},
	}
}

func TestProcessMaven_EmitsGetAndAdd(t *testing.T) {
	access := mavenTestAccess("4.0.1")
	resource := descriptorv2.Resource{}
	resource.Name = "spring-kafka"
	resource.Version = "4.0.1"
	resource.Type = "mavenArtifact"
	resource.Access = rawAccess(t, access)

	tgd := &transformv1alpha1.TransformationGraphDefinition{}
	toSpec := testOCIRepo("ghcr.io/target")
	ids := map[int]string{}

	err := processMaven(resource, access, "root", mavenTestValue(), tgd, toSpec, ids, 0)
	require.NoError(t, err)

	require.Len(t, tgd.Transformations, 2)
	assert.Equal(t, mavenv1alpha1.GetMavenArtifactV1alpha1, tgd.Transformations[0].Type)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, tgd.Transformations[1].Type)

	getID := tgd.Transformations[0].ID
	assert.Equal(t, "${"+getID+".output.file}", tgd.Transformations[1].Spec.Data["file"])
	assert.Equal(t, tgd.Transformations[1].ID, ids[0])

	// The Get node carries the resource itself: that is what the transformer
	// downloads, so a wrong key here would only surface at transfer time.
	getResource, ok := tgd.Transformations[0].Spec.Data["resource"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "spring-kafka", getResource["name"])
	getAccess, ok := getResource["access"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "maven/v2alpha1", getAccess["type"])
	assert.Equal(t, "4.0.1", getAccess["version"])

	// referenceName must stay an OCI repository name, so it is the resource name.
	addedResource, ok := tgd.Transformations[1].Spec.Data["resource"].(map[string]any)
	require.True(t, ok)
	addedAccess, ok := addedResource["access"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "spring-kafka", addedAccess["referenceName"])
}

func TestProcessMaven_RejectsUnpinnedVersions(t *testing.T) {
	for _, version := range []string{"LATEST", "RELEASE", "1.0-SNAPSHOT"} {
		access := mavenTestAccess(version)
		resource := descriptorv2.Resource{}
		resource.Name = "spring-kafka"
		resource.Version = version
		resource.Access = rawAccess(t, access)

		tgd := &transformv1alpha1.TransformationGraphDefinition{}
		err := processMaven(resource, access, "root", mavenTestValue(), tgd, testOCIRepo("ghcr.io/target"), map[int]string{}, 0)
		require.ErrorContains(t, err, version)
		require.ErrorContains(t, err, "pin")
		assert.Empty(t, tgd.Transformations)
	}
}
