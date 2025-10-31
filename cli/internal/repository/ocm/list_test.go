package ocm

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// mockComponentVersionRepository implements repository.ComponentVersionRepository for testing
type mockComponentVersionRepository struct {
	// Map of component name -> version -> descriptor
	data map[string]map[string]*descriptor.Descriptor
}

func newMockComponentVersionRepository() *mockComponentVersionRepository {
	return &mockComponentVersionRepository{
		data: make(map[string]map[string]*descriptor.Descriptor),
	}
}

func (m *mockComponentVersionRepository) AddComponentVersion(ctx context.Context, desc *descriptor.Descriptor) error {
	name := desc.Component.Name
	version := desc.Component.Version

	if m.data[name] == nil {
		m.data[name] = make(map[string]*descriptor.Descriptor)
	}
	m.data[name][version] = desc
	return nil
}

func (m *mockComponentVersionRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	versions := m.data[component]
	if versions == nil {
		return nil, fmt.Errorf("component not found: %s", component)
	}

	result := make([]string, 0, len(versions))
	for version := range versions {
		result = append(result, version)
	}
	return result, nil
}

func (m *mockComponentVersionRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	if m.data[component] == nil {
		return nil, fmt.Errorf("component not found: %s", component)
	}
	desc := m.data[component][version]
	if desc == nil {
		return nil, fmt.Errorf("version not found: %s:%s", component, version)
	}
	return desc, nil
}

func (m *mockComponentVersionRepository) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockComponentVersionRepository) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

func (m *mockComponentVersionRepository) AddLocalSource(ctx context.Context, component, version string, src *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockComponentVersionRepository) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

func (m *mockComponentVersionRepository) Close() error {
	return nil
}

// Helper function to create a descriptor for testing
func makeDescriptor(name, version string) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    name,
					Version: version,
				},
			},
		},
	}
}

func TestListComponentVersions(t *testing.T) {
	t.Run("SortEnabled_DefaultBehavior", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "1.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "2.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "1.5.0")))

		descs, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"test-component"}),
		)

		require.NoError(t, err)
		require.Len(t, descs, 3)
		// Verify descending order (newest first)
		require.Equal(t, "2.0.0", descs[0].Component.Version)
		require.Equal(t, "1.5.0", descs[1].Component.Version)
		require.Equal(t, "1.0.0", descs[2].Component.Version)
	})

	t.Run("MultipleComponents_Sorted", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-a", "1.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-a", "2.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-b", "1.5.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-b", "3.0.0")))

		descs, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"comp-a", "comp-b"}),
		)

		require.NoError(t, err)
		require.Len(t, descs, 4)
		// Verify global sorting across all components
		require.Equal(t, "3.0.0", descs[0].Component.Version)
		require.Equal(t, "2.0.0", descs[1].Component.Version)
		require.Equal(t, "1.5.0", descs[2].Component.Version)
		require.Equal(t, "1.0.0", descs[3].Component.Version)
	})

	t.Run("WithSemverConstraint", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "1.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "2.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "2.5.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "3.0.0")))

		descs, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"test-component"}),
			WithSemverConstraint(">= 2.0.0, < 3.0.0"),
		)

		require.NoError(t, err)
		require.Len(t, descs, 2)
		// Verify only versions matching constraint are returned, sorted
		require.Equal(t, "2.5.0", descs[0].Component.Version)
		require.Equal(t, "2.0.0", descs[1].Component.Version)
	})

	t.Run("WithLatestOnly", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "1.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "2.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "1.5.0")))

		descs, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"test-component"}),
			WithLatestOnly(true),
		)

		require.NoError(t, err)
		require.Len(t, descs, 1)
		// Verify only the latest version is returned
		require.Equal(t, "2.0.0", descs[0].Component.Version)
	})

	t.Run("WithLatestOnly_MultipleComponents", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-a", "1.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-a", "2.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-b", "1.5.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-b", "3.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-c", "3.0.0")))

		descs, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"comp-a", "comp-b"}),
			WithLatestOnly(true),
		)

		require.NoError(t, err)
		require.Len(t, descs, 2)
		// Verify latest from each component is returned
		versions := make([]string, len(descs))
		for i, d := range descs {
			versions[i] = d.Component.Version
		}
		require.Contains(t, versions, "2.0.0")
		require.Contains(t, versions, "3.0.0")
	})

	t.Run("WithLatestOnly_NoVersionsAvailable", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()
		// Create a component with no versions (empty version map)
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-c", "3.0.0")))

		descs, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"comp-c"}),
			WithSemverConstraint("< 2.0.0"),
			WithLatestOnly(true),
		)

		require.NoError(t, err)
		require.Len(t, descs, 0)
	})

	t.Run("EmptyComponentList", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()

		descs, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{}),
		)

		require.NoError(t, err)
		require.Nil(t, descs)
	})

	t.Run("ComponentNotFound", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()

		_, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"non-existent"}),
		)

		require.Error(t, err)
		require.Contains(t, err.Error(), "listing component versions failed")
	})

	t.Run("WithConcurrencyLimit", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-a", "1.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("comp-b", "2.0.0")))

		descs, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"comp-a", "comp-b"}),
			WithConcurrencyLimit(1),
		)

		require.NoError(t, err)
		require.Len(t, descs, 2)
		// Verify both components are retrieved despite concurrency limit
		versions := make([]string, len(descs))
		for i, d := range descs {
			versions[i] = d.Component.Version
		}
		require.Contains(t, versions, "1.0.0")
		require.Contains(t, versions, "2.0.0")
	})

	t.Run("ComplexSorting_MixedVersions", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("app-a", "0.1.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("app-a", "10.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("app-a", "2.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("app-b", "1.0.0")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("app-b", "1.2.3")))
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("app-b", "1.10.0")))

		descs, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"app-a", "app-b"}),
		)

		require.NoError(t, err)
		require.Len(t, descs, 6)
		// Verify correct semantic version sorting (not lexicographic)
		require.Equal(t, "10.0.0", descs[0].Component.Version)
		require.Equal(t, "2.0.0", descs[1].Component.Version)
		require.Equal(t, "1.10.0", descs[2].Component.Version)
		require.Equal(t, "1.2.3", descs[3].Component.Version)
		require.Equal(t, "1.0.0", descs[4].Component.Version)
		require.Equal(t, "0.1.0", descs[5].Component.Version)
	})

	t.Run("InvalidSemverConstraint", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "1.0.0")))

		_, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"test-component"}),
			WithSemverConstraint("invalid-constraint"),
		)

		require.Error(t, err)
		require.Contains(t, err.Error(), "filtering component versions failed")
	})

	t.Run("InvalidSemver", func(t *testing.T) {
		ctx := context.Background()
		repo := newMockComponentVersionRepository()
		require.NoError(t, repo.AddComponentVersion(ctx, makeDescriptor("test-component", "latest")))

		_, err := ListComponentVersions(ctx, repo,
			WithComponentNames([]string{"test-component"}),
		)

		require.Error(t, err)
		require.Contains(t, err.Error(), "found invalid semver version: parsing version \"latest\" failed: invalid semantic version")
	})
}
