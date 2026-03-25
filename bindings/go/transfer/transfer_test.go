package transfer

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// --- mock types ---

type mockCVRepo struct {
	repository.ComponentVersionRepository // embed to satisfy the interface
	descriptors                           map[string]*descriptor.Descriptor
}

func (m *mockCVRepo) GetComponentVersion(_ context.Context, component, version string) (*descriptor.Descriptor, error) {
	key := component + ":" + version
	d, ok := m.descriptors[key]
	if !ok {
		return nil, fmt.Errorf("component version %s not found", key)
	}
	return d, nil
}

type mockCVRepoResolver struct {
	specs      map[string]runtime.Typed
	repos      map[string]repository.ComponentVersionRepository
	sharedRepo repository.ComponentVersionRepository
	err        error
}

func (m *mockCVRepoResolver) GetRepositorySpecificationForComponent(_ context.Context, component, version string) (runtime.Typed, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := component + ":" + version
	spec, ok := m.specs[key]
	if !ok {
		return nil, fmt.Errorf("no spec for %s", key)
	}
	return spec, nil
}

func (m *mockCVRepoResolver) GetComponentVersionRepositoryForSpecification(_ context.Context, _ runtime.Typed) (repository.ComponentVersionRepository, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.sharedRepo != nil {
		return m.sharedRepo, nil
	}
	return nil, fmt.Errorf("no repos configured and sharedRepo is nil")
}

func (m *mockCVRepoResolver) GetComponentVersionRepositoryForComponent(_ context.Context, component, version string) (repository.ComponentVersionRepository, error) {
	if m.err != nil {
		return nil, m.err
	}
	key := component + ":" + version
	r, ok := m.repos[key]
	if !ok {
		return nil, fmt.Errorf("no repo for %s", key)
	}
	return r, nil
}

var _ resolvers.ComponentVersionRepositoryResolver = (*mockCVRepoResolver)(nil)

// --- test helpers ---

func testOCITarget(baseURL string) *oci.Repository {
	return &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: baseURL,
	}
}

func testDescriptor(component, version string, refs []descriptor.Reference) *descriptor.Descriptor {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    component,
					Version: version,
				},
			},
			Provider:   descriptor.Provider{Name: "test-provider"},
			References: refs,
		},
	}
}

func testResolverFor(component, version string, repoSpec runtime.Typed, desc *descriptor.Descriptor) *mockCVRepoResolver {
	key := component + ":" + version
	repo := &mockCVRepo{
		descriptors: map[string]*descriptor.Descriptor{key: desc},
	}
	return &mockCVRepoResolver{
		specs:      map[string]runtime.Typed{key: repoSpec},
		repos:      map[string]repository.ComponentVersionRepository{key: repo},
		sharedRepo: repo,
	}
}

func testMultiComponentResolver(entries map[string]struct {
	spec runtime.Typed
	desc *descriptor.Descriptor
}) *mockCVRepoResolver {
	specs := make(map[string]runtime.Typed)
	allDescs := make(map[string]*descriptor.Descriptor)
	for key, entry := range entries {
		specs[key] = entry.spec
		allDescs[key] = entry.desc
	}
	sharedRepo := &mockCVRepo{descriptors: allDescs}
	repos := make(map[string]repository.ComponentVersionRepository)
	for key := range entries {
		repos[key] = sharedRepo
	}
	return &mockCVRepoResolver{specs: specs, repos: repos, sharedRepo: sharedRepo}
}

// --- Happy path tests ---

func TestBuildGraphDefinition_SingleMapping(t *testing.T) {
	sourceRepo := testOCITarget("ghcr.io/source")
	targetRepo := testOCITarget("ghcr.io/target")
	desc := testDescriptor("ocm.software/test", "1.0.0", nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)

	tgd, err := BuildGraphDefinition(t.Context(),
		WithTransfer(
			Component("ocm.software/test", "1.0.0"),
			ToRepositorySpec(targetRepo),
			FromResolver(resolver),
		),
	)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	assert.NotNil(t, tgd.Environment)
	require.NotEmpty(t, tgd.Transformations)

	// There should be at least one upload transformation.
	uploadCount := 0
	for _, tr := range tgd.Transformations {
		if strings.Contains(tr.ID, "Upload") {
			uploadCount++
		}
	}
	assert.Equal(t, 1, uploadCount, "expected exactly 1 upload transformation")
}

func TestBuildGraphDefinition_MultipleComponentsSameTarget(t *testing.T) {
	sourceRepo := testOCITarget("ghcr.io/source")
	targetRepo := testOCITarget("ghcr.io/target")

	descA := testDescriptor("ocm.software/a", "1.0.0", nil)
	descB := testDescriptor("ocm.software/b", "2.0.0", nil)

	resolver := testMultiComponentResolver(map[string]struct {
		spec runtime.Typed
		desc *descriptor.Descriptor
	}{
		"ocm.software/a:1.0.0": {spec: sourceRepo, desc: descA},
		"ocm.software/b:2.0.0": {spec: sourceRepo, desc: descB},
	})

	tgd, err := BuildGraphDefinition(t.Context(),
		WithTransfer(
			Component("ocm.software/a", "1.0.0"),
			Component("ocm.software/b", "2.0.0"),
			ToRepositorySpec(targetRepo),
			FromResolver(resolver),
		),
	)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	uploadCount := 0
	for _, tr := range tgd.Transformations {
		if strings.Contains(tr.ID, "Upload") {
			uploadCount++
		}
	}
	assert.Equal(t, 2, uploadCount, "expected 2 upload transformations, one per component")
}

func TestBuildGraphDefinition_DifferentComponentsDifferentTargets(t *testing.T) {
	sourceA := testOCITarget("ghcr.io/source-a")
	sourceB := testOCITarget("ghcr.io/source-b")
	targetA := testOCITarget("ghcr.io/target-a")
	targetB := testOCITarget("ghcr.io/target-b")

	descA := testDescriptor("ocm.software/a", "1.0.0", nil)
	descB := testDescriptor("ocm.software/b", "2.0.0", nil)

	resolverA := testResolverFor("ocm.software/a", "1.0.0", sourceA, descA)
	resolverB := testResolverFor("ocm.software/b", "2.0.0", sourceB, descB)

	tgd, err := BuildGraphDefinition(t.Context(),
		WithTransfer(
			Component("ocm.software/a", "1.0.0"),
			ToRepositorySpec(targetA),
			FromResolver(resolverA),
		),
		WithTransfer(
			Component("ocm.software/b", "2.0.0"),
			ToRepositorySpec(targetB),
			FromResolver(resolverB),
		),
	)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	uploadCount := 0
	for _, tr := range tgd.Transformations {
		if strings.Contains(tr.ID, "Upload") {
			uploadCount++
		}
	}
	assert.Equal(t, 2, uploadCount, "expected 2 upload transformations, one per component/target pair")
	assert.Len(t, tgd.Transformations, 2, "expected exactly 2 transformations total (upload only, no resources)")
}

func TestBuildGraphDefinition_SameComponentMultipleTargets(t *testing.T) {
	sourceRepo := testOCITarget("ghcr.io/source")
	target1 := testOCITarget("ghcr.io/target1")
	target2 := testOCITarget("ghcr.io/target2")

	desc := testDescriptor("ocm.software/test", "1.0.0", nil)
	resolver := testResolverFor("ocm.software/test", "1.0.0", sourceRepo, desc)

	tgd, err := BuildGraphDefinition(t.Context(),
		WithTransfer(
			Component("ocm.software/test", "1.0.0"),
			ToRepositorySpec(target1),
			FromResolver(resolver),
		),
		WithTransfer(
			Component("ocm.software/test", "1.0.0"),
			ToRepositorySpec(target2),
			FromResolver(resolver),
		),
	)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	// Same component sent to 2 targets should produce 2 upload transformations.
	uploadCount := 0
	for _, tr := range tgd.Transformations {
		if strings.Contains(tr.ID, "Upload") {
			uploadCount++
		}
	}
	assert.Equal(t, 2, uploadCount, "expected 2 upload transformations for 2 different targets")

	// Verify the IDs are different (target-suffixed when multiple targets).
	if len(tgd.Transformations) >= 2 {
		assert.NotEqual(t, tgd.Transformations[0].ID, tgd.Transformations[1].ID,
			"upload IDs should differ when targeting multiple repositories")
	}
}

func TestBuildGraphDefinition_FromRepository(t *testing.T) {
	sourceSpec := testOCITarget("ghcr.io/source")
	targetRepo := testOCITarget("ghcr.io/target")

	desc := testDescriptor("ocm.software/test", "1.0.0", nil)
	mockRepo := &mockCVRepo{
		descriptors: map[string]*descriptor.Descriptor{
			"ocm.software/test:1.0.0": desc,
		},
	}

	tgd, err := BuildGraphDefinition(t.Context(),
		WithTransfer(
			Component("ocm.software/test", "1.0.0"),
			ToRepositorySpec(targetRepo),
			FromRepository(mockRepo, sourceSpec),
		),
	)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	uploadCount := 0
	for _, tr := range tgd.Transformations {
		if strings.Contains(tr.ID, "Upload") {
			uploadCount++
		}
	}
	assert.Equal(t, 1, uploadCount, "expected 1 upload transformation when using FromRepository")
}

func TestBuildGraphDefinition_WithTransferFromLister(t *testing.T) {
	sourceRepo := testOCITarget("ghcr.io/source")
	targetRepo := testOCITarget("ghcr.io/target")

	descA := testDescriptor("ocm.software/a", "1.0.0", nil)
	descB := testDescriptor("ocm.software/b", "2.0.0", nil)

	resolver := testMultiComponentResolver(map[string]struct {
		spec runtime.Typed
		desc *descriptor.Descriptor
	}{
		"ocm.software/a:1.0.0": {spec: sourceRepo, desc: descA},
		"ocm.software/b:2.0.0": {spec: sourceRepo, desc: descB},
	})

	lister := ComponentListerFunc(func(_ context.Context, fn func(ids []ComponentID) error) error {
		return fn([]ComponentID{
			{Component: "ocm.software/a", Version: "1.0.0"},
			{Component: "ocm.software/b", Version: "2.0.0"},
		})
	})

	tgd, err := BuildGraphDefinition(t.Context(),
		WithTransfer(FromLister(lister), ToRepositorySpec(targetRepo), FromResolver(resolver)),
	)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	uploadCount := 0
	for _, tr := range tgd.Transformations {
		if strings.Contains(tr.ID, "Upload") {
			uploadCount++
		}
	}
	assert.Equal(t, 2, uploadCount, "expected 2 upload transformations from lister-resolved components")
}

func TestBuildGraphDefinition_Recursive(t *testing.T) {
	sourceRepo := testOCITarget("ghcr.io/source")
	targetRepo := testOCITarget("ghcr.io/target")

	childDesc := testDescriptor("ocm.software/child", "2.0.0", nil)
	parentDesc := testDescriptor("ocm.software/parent", "1.0.0", []descriptor.Reference{
		{
			ElementMeta: descriptor.ElementMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "child-ref", Version: "2.0.0"},
			},
			Component: "ocm.software/child",
		},
	})

	resolver := testMultiComponentResolver(map[string]struct {
		spec runtime.Typed
		desc *descriptor.Descriptor
	}{
		"ocm.software/parent:1.0.0": {spec: sourceRepo, desc: parentDesc},
		"ocm.software/child:2.0.0":  {spec: sourceRepo, desc: childDesc},
	})

	tgd, err := BuildGraphDefinition(t.Context(),
		WithTransfer(
			Component("ocm.software/parent", "1.0.0"),
			ToRepositorySpec(targetRepo),
			FromResolver(resolver),
		),
		WithRecursive(true),
	)
	require.NoError(t, err)
	require.NotNil(t, tgd)

	// Both parent and child should be transferred.
	uploadCount := 0
	for _, tr := range tgd.Transformations {
		if strings.Contains(tr.ID, "Upload") {
			uploadCount++
		}
	}
	assert.Equal(t, 2, uploadCount, "expected 2 upload transformations: parent + child")
}

// --- Validation error tests ---

func TestBuildGraphDefinition_NoMappings(t *testing.T) {
	_, err := BuildGraphDefinition(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no transfer mappings")
}

func TestBuildGraphDefinition_MissingTarget(t *testing.T) {
	resolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}
	_, err := BuildGraphDefinition(t.Context(),
		WithTransfer(
			Component("ocm.software/test", "1.0.0"),
			FromResolver(resolver),
		),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no target")
}

func TestBuildGraphDefinition_MissingResolver(t *testing.T) {
	targetRepo := testOCITarget("ghcr.io/target")
	_, err := BuildGraphDefinition(t.Context(),
		WithTransfer(
			Component("ocm.software/test", "1.0.0"),
			ToRepositorySpec(targetRepo),
		),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no resolver")
}

func TestBuildGraphDefinition_MissingComponents(t *testing.T) {
	targetRepo := testOCITarget("ghcr.io/target")
	resolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}
	_, err := BuildGraphDefinition(t.Context(),
		WithTransfer(
			ToRepositorySpec(targetRepo),
			FromResolver(resolver),
		),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no components")
}

func TestBuildGraphDefinition_ListerAndComponentsConflict(t *testing.T) {
	targetRepo := testOCITarget("ghcr.io/target")
	resolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}

	lister := ComponentListerFunc(func(_ context.Context, fn func(ids []ComponentID) error) error {
		return fn([]ComponentID{{Component: "ocm.software/a", Version: "1.0.0"}})
	})

	// Build a mapping that has both a lister and explicit components.
	var o Options
	o.Mappings = append(o.Mappings, Mapping{
		Components: []ComponentID{
			{Component: "ocm.software/b", Version: "2.0.0"},
		},
		ComponentLister: lister,
		Target:          targetRepo,
		Resolver:        resolver,
	})

	_, err := collectTransferRoots(t.Context(), &o)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot combine")
}

func TestBuildGraphDefinition_EmptyListerResult(t *testing.T) {
	targetRepo := testOCITarget("ghcr.io/target")
	resolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}

	lister := ComponentListerFunc(func(_ context.Context, fn func(ids []ComponentID) error) error {
		// Return an empty slice.
		return fn([]ComponentID{})
	})

	_, err := BuildGraphDefinition(t.Context(),
		WithTransfer(FromLister(lister), ToRepositorySpec(targetRepo), FromResolver(resolver)),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no components")
}

// --- Internal helper tests ---

func TestCollectTransferRoots_SingleMapping(t *testing.T) {
	targetRepo := testOCITarget("ghcr.io/target")
	resolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}

	o := &Options{
		Mappings: []Mapping{
			{
				Components: []ComponentID{
					{Component: "ocm.software/test", Version: "1.0.0"},
				},
				Target:   targetRepo,
				Resolver: resolver,
			},
		},
	}

	roots, err := collectTransferRoots(t.Context(), o)
	require.NoError(t, err)
	require.Len(t, roots, 1)
	assert.Equal(t, "ocm.software/test:1.0.0", roots[0].Key)
	assert.Equal(t, []runtime.Typed{targetRepo}, roots[0].Targets)
	assert.Equal(t, resolver, roots[0].Resolver)
}

func TestCollectTransferRoots_MergesTargetsForSameComponent(t *testing.T) {
	target1 := testOCITarget("ghcr.io/target1")
	target2 := testOCITarget("ghcr.io/target2")
	resolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}

	o := &Options{
		Mappings: []Mapping{
			{
				Components: []ComponentID{
					{Component: "ocm.software/test", Version: "1.0.0"},
				},
				Target:   target1,
				Resolver: resolver,
			},
			{
				Components: []ComponentID{
					{Component: "ocm.software/test", Version: "1.0.0"},
				},
				Target:   target2,
				Resolver: resolver,
			},
		},
	}

	roots, err := collectTransferRoots(t.Context(), o)
	require.NoError(t, err)
	require.Len(t, roots, 1, "same component should be de-duplicated into one root")
	assert.Len(t, roots[0].Targets, 2, "should have 2 targets merged")
	assert.Contains(t, roots[0].Targets, runtime.Typed(target1))
	assert.Contains(t, roots[0].Targets, runtime.Typed(target2))
}

func TestCollectTransferRoots_DuplicateTargetNotMergedTwice(t *testing.T) {
	targetRepo := testOCITarget("ghcr.io/target")
	resolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}

	o := &Options{
		Mappings: []Mapping{
			{
				Components: []ComponentID{
					{Component: "ocm.software/test", Version: "1.0.0"},
				},
				Target:   targetRepo,
				Resolver: resolver,
			},
			{
				Components: []ComponentID{
					{Component: "ocm.software/test", Version: "1.0.0"},
				},
				Target:   targetRepo, // same pointer
				Resolver: resolver,
			},
		},
	}

	roots, err := collectTransferRoots(t.Context(), o)
	require.NoError(t, err)
	require.Len(t, roots, 1)
	assert.Len(t, roots[0].Targets, 1, "duplicate target (same pointer) should not be added twice")
}

func TestResolveMapping_WithComponents(t *testing.T) {
	m := &Mapping{
		Components: []ComponentID{
			{Component: "ocm.software/a", Version: "1.0.0"},
			{Component: "ocm.software/b", Version: "2.0.0"},
		},
	}
	ids, err := resolveMapping(t.Context(), m)
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Equal(t, "ocm.software/a", ids[0].Component)
	assert.Equal(t, "ocm.software/b", ids[1].Component)
}

func TestResolveMapping_WithLister(t *testing.T) {
	lister := ComponentListerFunc(func(_ context.Context, fn func(ids []ComponentID) error) error {
		return fn([]ComponentID{
			{Component: "ocm.software/listed", Version: "3.0.0"},
		})
	})
	m := &Mapping{
		ComponentLister: lister,
	}
	ids, err := resolveMapping(t.Context(), m)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	assert.Equal(t, "ocm.software/listed", ids[0].Component)
	assert.Equal(t, "3.0.0", ids[0].Version)
}

func TestResolveMapping_ListerError(t *testing.T) {
	lister := ComponentListerFunc(func(_ context.Context, _ func(ids []ComponentID) error) error {
		return fmt.Errorf("listing failed")
	})
	m := &Mapping{
		ComponentLister: lister,
	}
	_, err := resolveMapping(t.Context(), m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "listing failed")
}

func TestResolveMapping_EmptyLister(t *testing.T) {
	lister := ComponentListerFunc(func(_ context.Context, fn func(ids []ComponentID) error) error {
		return fn([]ComponentID{})
	})
	m := &Mapping{
		ComponentLister: lister,
	}
	_, err := resolveMapping(t.Context(), m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no components")
}

func TestResolveMapping_NoComponentsNoLister(t *testing.T) {
	m := &Mapping{}
	_, err := resolveMapping(t.Context(), m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no components")
}

func TestResolveMapping_Conflict(t *testing.T) {
	lister := ComponentListerFunc(func(_ context.Context, fn func(ids []ComponentID) error) error {
		return fn([]ComponentID{{Component: "a", Version: "1"}})
	})
	m := &Mapping{
		Components:      []ComponentID{{Component: "b", Version: "2"}},
		ComponentLister: lister,
	}
	_, err := resolveMapping(t.Context(), m)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot combine")
}
