package v1alpha1_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var MockType = runtime.NewUnversionedType("mock-repository")

const (
	PolicyErrorOnGetRepositoryForSpec     = "fail-get-repository-for-spec"
	PolicyReturnNilOnGetRepositoryForSpec = "nil-get-repository-for-spec"
)

type RepositorySpec struct {
	Type runtime.Type `json:"type"`

	// Name is used for identification of the mock repository.
	Name string

	// Components is a map of component names to a list of component versions
	// that are available in this mock repository.
	Components map[string][]string

	// Policy defines additional behavior of the mock repository.
	Policy string
}

func (r *RepositorySpec) GetType() runtime.Type {
	return r.Type
}

func (r *RepositorySpec) SetType(t runtime.Type) {
	r.Type = t
}

func (r *RepositorySpec) DeepCopyTyped() runtime.Typed {
	return &RepositorySpec{
		Type:       r.Type,
		Name:       r.Name,
		Components: maps.Clone(r.Components),
		Policy:     r.Policy,
	}
}

var _ runtime.Typed = (*RepositorySpec)(nil)

func NewRepositorySpecRaw(t *testing.T, name string, components map[string][]string, failPolicy ...string) *runtime.Raw {
	repoSpec := &RepositorySpec{
		Type:       MockType,
		Name:       name,
		Components: components,
	}
	if len(failPolicy) == 1 {
		repoSpec.Policy = failPolicy[0]
	}

	j, err := json.Marshal(repoSpec)
	require.NoError(t, err)

	raw := &runtime.Raw{
		Type: MockType,
		Data: j,
	}

	return raw
}

type MockProvider struct{}

func (m MockProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (m MockProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
	switch spec := repositorySpecification.(type) {
	case *RepositorySpec:
		switch spec.Policy {
		case PolicyErrorOnGetRepositoryForSpec:
			return nil, fmt.Errorf("mock error for testing: %s", spec.Policy)
		case PolicyReturnNilOnGetRepositoryForSpec:
			return nil, nil
		}
		return &MockRepository{
			RepositorySpec: spec,
		}, nil
	case *runtime.Raw:
		var s RepositorySpec
		err := json.Unmarshal(spec.Data, &s)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal repository spec: %w", err)
		}
		switch s.Policy {
		case PolicyErrorOnGetRepositoryForSpec:
			return nil, fmt.Errorf("mock error for testing: %s", s.Policy)
		case PolicyReturnNilOnGetRepositoryForSpec:
			return nil, nil
		}
		return &MockRepository{
			RepositorySpec: &s,
		}, nil
	default:
		panic(fmt.Sprintf("unexpected repository specification type: %T", repositorySpecification))
	}
}

type MockRepository struct {
	typ runtime.Type
	*RepositorySpec
}

func (m MockRepository) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) AddLocalSource(ctx context.Context, component, version string, res *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	//TODO implement me
	panic("implement me")
}

func (m MockRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	//TODO implement me
	panic("implement me")
}
