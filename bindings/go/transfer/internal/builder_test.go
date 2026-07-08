package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

type stubRepoProvider struct {
	repository.ComponentVersionRepositoryProvider
}

type stubResourceRepo struct {
	repository.ResourceRepository
}

type stubCredResolver struct {
	credentials.Resolver
}

func TestNewBuilder(t *testing.T) {
	b := NewDefaultBuilder(&stubRepoProvider{}, &stubResourceRepo{}, &stubCredResolver{})
	assert.NotNil(t, b)
}

func TestNewDefaultBuilder_CanBuildGitHubGraph(t *testing.T) {
	sourceRepo := testOCIRepo("ghcr.io/source")
	targetRepo := testOCIRepo("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0",
		[]descriptor.Resource{githubResource("my-source", "1.0.0",
			"https://github.com/octocat/Hello-World",
			"7fd1a60b01f91b314f59955a4e4d4e80d8edf11d")}, nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)
	roots := testTransferRoots("ocm.software/test", "1.0.0", targetRepo, resolver)
	tgd, err := BuildGraphDefinition(t.Context(), roots, transferv1alpha1.Config{
		CopyMode: transferv1alpha1.CopyModeAllResources, UploadType: transferv1alpha1.UploadAsDefault,
	})
	require.NoError(t, err)

	// BuildAndCheck validates that every transformation type in the graph has a
	// registered transformer. Providers are only used at Process time, so nil is fine.
	_, err = NewDefaultBuilder(nil, nil, nil).BuildAndCheck(tgd)
	require.NoError(t, err, "default builder must resolve the GetGitHubCommit transformer")
}
