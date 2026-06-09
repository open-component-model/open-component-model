package transfer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1"
)

func TestDefaultOptions(t *testing.T) {
	var o Options
	assert.Equal(t, transferv1alpha1.CopyMode(""), o.CopyMode)
	assert.Equal(t, transferv1alpha1.UploadType(""), o.UploadType)
	assert.Equal(t, transferv1alpha1.RecursiveNone, o.Recursive)
	assert.Equal(t, transferv1alpha1.CopyModeLocalBlobResources, o.GetCopyMode())
	assert.Equal(t, transferv1alpha1.UploadAsDefault, o.GetUploadType())
	assert.Equal(t, 0, o.GetRecursive())
}

func TestWithCopyMode(t *testing.T) {
	var o Options
	WithCopyMode(transferv1alpha1.CopyModeAllResources)(&o)
	assert.Equal(t, transferv1alpha1.CopyModeAllResources, o.CopyMode)
}

func TestWithRecursive(t *testing.T) {
	var o Options
	WithRecursive(-1)(&o)
	assert.Equal(t, transferv1alpha1.RecursiveInfinite, o.Recursive)

	WithRecursive(0)(&o)
	assert.Equal(t, transferv1alpha1.RecursiveNone, o.Recursive, "WithRecursive(0) must overwrite the previous WithRecursive(-1) - that's the override path")
}

func TestWithUploadType(t *testing.T) {
	var o Options
	WithUploadType(transferv1alpha1.UploadAsOciArtifact)(&o)
	assert.Equal(t, transferv1alpha1.UploadAsOciArtifact, o.UploadType)
}

type mockRepo struct {
	repository.ComponentVersionRepository
}

type mockResolver struct {
	resolvers.ComponentVersionRepositoryResolver
}

func TestComponentID_String(t *testing.T) {
	id := ComponentID{Component: "ocm.software/test", Version: "1.0.0"}
	assert.Equal(t, "ocm.software/test:1.0.0", id.String())
}

func TestComponentListerFunc(t *testing.T) {
	expected := []ComponentID{
		{Component: "ocm.software/a", Version: "1.0.0"},
		{Component: "ocm.software/b", Version: "2.0.0"},
	}
	lister := ComponentListerFunc(func(_ context.Context, fn func(ids []ComponentID) error) error {
		return fn(expected)
	})
	var got []ComponentID
	err := lister.ListComponentVersions(t.Context(), func(ids []ComponentID) error {
		got = append(got, ids...)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, expected, got)
}

func TestWithTransfer_BuildsMapping(t *testing.T) {
	target := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io/target",
	}
	resolver := &mockResolver{}
	var o Options
	WithTransfer(
		Component("ocm.software/a", "1.0.0"),
		ToRepositorySpec(target),
		FromResolver(resolver),
	)(&o)
	require.Len(t, o.Mappings, 1)
	assert.Equal(t, []ComponentID{{Component: "ocm.software/a", Version: "1.0.0"}}, o.Mappings[0].Components)
	assert.Equal(t, target, o.Mappings[0].Target)
	assert.Equal(t, resolver, o.Mappings[0].Resolver)
}

func TestWithTransfer_MultipleComponents(t *testing.T) {
	target := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io/target",
	}
	resolver := &mockResolver{}
	var o Options
	WithTransfer(
		Component("ocm.software/a", "1.0.0"),
		Component("ocm.software/b", "2.0.0"),
		ToRepositorySpec(target),
		FromResolver(resolver),
	)(&o)
	require.Len(t, o.Mappings, 1)
	assert.Len(t, o.Mappings[0].Components, 2)
	assert.Equal(t, ComponentID{Component: "ocm.software/a", Version: "1.0.0"}, o.Mappings[0].Components[0])
	assert.Equal(t, ComponentID{Component: "ocm.software/b", Version: "2.0.0"}, o.Mappings[0].Components[1])
}

func TestFromRepository_WrapsRepo(t *testing.T) {
	repo := &mockRepo{}
	spec := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io"}
	var m Mapping
	FromRepository(repo, spec)(&m)
	assert.NotNil(t, m.Resolver)
}

func TestRepoResolver_ReturnsRepo(t *testing.T) {
	repo := &mockRepo{}
	spec := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io"}
	r := &repoResolver{repo: repo, spec: spec}

	gotRepo, err := r.GetComponentVersionRepositoryForComponent(t.Context(), "ocm.software/test", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, repo, gotRepo)

	gotRepo, err = r.GetComponentVersionRepositoryForSpecification(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, repo, gotRepo)

	gotSpec, err := r.GetRepositorySpecificationForComponent(t.Context(), "ocm.software/test", "1.0.0")
	require.NoError(t, err)
	assert.Equal(t, spec, gotSpec)
}

func TestFromConfig_Nil(t *testing.T) {
	require.Nil(t, FromConfig(nil))
}

func TestFromConfig_AllFields(t *testing.T) {
	cfg := &transferv1alpha1.Config{
		Recursive:  -1,
		CopyMode:   transferv1alpha1.CopyModeAllResources,
		UploadType: transferv1alpha1.UploadAsOciArtifact,
	}
	var o Options
	for _, opt := range FromConfig(cfg) {
		opt(&o)
	}
	require.Equal(t, -1, o.GetRecursive())
	require.Equal(t, transferv1alpha1.CopyModeAllResources, o.CopyMode)
	require.Equal(t, transferv1alpha1.UploadAsOciArtifact, o.UploadType)
}

func TestFromConfig_PartialDoesNotClobber(t *testing.T) {
	// Empty config fields are skipped so prior overrides survive.
	cfg := &transferv1alpha1.Config{CopyMode: transferv1alpha1.CopyModeAllResources}

	o := Options{Config: transferv1alpha1.Config{Recursive: -1, UploadType: transferv1alpha1.UploadAsLocalBlob}}
	for _, opt := range FromConfig(cfg) {
		opt(&o)
	}
	require.Equal(t, 0, o.GetRecursive())
	require.Equal(t, transferv1alpha1.CopyModeAllResources, o.CopyMode)
	require.Equal(t, transferv1alpha1.UploadAsLocalBlob, o.UploadType)
}
