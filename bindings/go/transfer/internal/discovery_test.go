package internal

import (
	"context"
	"fmt"
	"sync"
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
	repository.ComponentVersionRepository
	descriptors map[string]*descriptor.Descriptor
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
	sharedRepo repository.ComponentVersionRepository // returned by ForSpecification when set
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
	// Return the first repo (for simple tests)
	for _, r := range m.repos {
		return r, nil
	}
	return nil, fmt.Errorf("no repos configured")
}

func (m *mockCVRepoResolver) GetComponentVersionRepositoryForComponent(_ context.Context, component, version string) (repository.ComponentVersionRepository, error) {
	key := component + ":" + version
	r, ok := m.repos[key]
	if !ok {
		return nil, fmt.Errorf("no repo for %s", key)
	}
	return r, nil
}

// --- identityToTransformationID tests ---

func TestIdentityToTransformationID(t *testing.T) {
	tests := []struct {
		name     string
		identity runtime.Identity
		want     string
	}{
		{
			name:     "single key",
			identity: runtime.Identity{"name": "mycomponent"},
			want:     "transformMycomponent",
		},
		{
			name: "name and version sorted by key",
			identity: runtime.Identity{
				descriptor.IdentityAttributeName:    "ocm.software/test",
				descriptor.IdentityAttributeVersion: "1.0.0",
			},
			// keys sorted: "name" < "version", so name values come first
			want: "transformOcmSoftwareTest100",
		},
		{
			name: "with dots and slashes",
			identity: runtime.Identity{
				"name": "ocm.software/my-component",
			},
			want: "transformOcmSoftwareMyComponent",
		},
		{
			name:     "empty identity",
			identity: runtime.Identity{},
			want:     "transform",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := identityToTransformationID(tt.identity)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- discoverer tests ---

func TestDiscoverer_NonRecursive(t *testing.T) {
	d := &discoverer{recursive: false, discoveredDigests: make(map[string]descriptor.Digest)}
	parent := &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				References: []descriptor.Reference{
					{
						ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "ref1", Version: "1.0.0"}},
						Component:   "ocm.software/child",
					},
				},
			},
		},
	}
	children, err := d.Discover(t.Context(), parent)
	require.NoError(t, err)
	assert.Nil(t, children)
}

func TestDiscoverer_Recursive(t *testing.T) {
	d := &discoverer{recursive: true, discoveredDigests: make(map[string]descriptor.Digest)}
	parent := &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				References: []descriptor.Reference{
					{
						ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "ref1", Version: "2.0.0"}},
						Component:   "ocm.software/child",
						Digest: descriptor.Digest{
							HashAlgorithm:          "SHA-256",
							NormalisationAlgorithm: "jsonNormalisation/v1",
							Value:                  "abc123",
						},
					},
				},
			},
		},
	}
	children, err := d.Discover(t.Context(), parent)
	require.NoError(t, err)
	assert.Equal(t, []string{"ocm.software/child:2.0.0"}, children)
	assert.Len(t, d.discoveredDigests, 1)
}

// --- resolver tests ---

func TestResolver_ValidKey(t *testing.T) {
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/test", Version: "1.0.0"},
			},
		},
	}

	repoSpec := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
	}

	mockResolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{
			"ocm.software/test:1.0.0": repoSpec,
		},
		repos: map[string]repository.ComponentVersionRepository{
			"ocm.software/test:1.0.0": &mockCVRepo{
				descriptors: map[string]*descriptor.Descriptor{
					"ocm.software/test:1.0.0": desc,
				},
			},
		},
	}
	r := &multiResolver{
		mu: &sync.Mutex{},
		resolverMap: map[string]resolvers.ComponentVersionRepositoryResolver{
			"ocm.software/test:1.0.0": mockResolver,
		},
		expectedDigest: func(_ runtime.Identity) *descriptor.Digest { return nil },
	}

	val, err := r.Resolve(t.Context(), "ocm.software/test:1.0.0")
	require.NoError(t, err)
	assert.Equal(t, desc, val.Descriptor)
	assert.Equal(t, repoSpec, val.SourceRepository)
}

func TestResolver_InvalidKeyFormat(t *testing.T) {
	r := &multiResolver{
		mu:             &sync.Mutex{},
		resolverMap:    map[string]resolvers.ComponentVersionRepositoryResolver{},
		expectedDigest: func(_ runtime.Identity) *descriptor.Digest { return nil },
	}
	_, err := r.Resolve(t.Context(), "invalid-no-colon")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid key format")
}

func TestResolver_RepoSpecError(t *testing.T) {
	mockRes := &mockCVRepoResolver{
		err:   fmt.Errorf("spec lookup failed"),
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}
	r := &multiResolver{
		mu: &sync.Mutex{},
		resolverMap: map[string]resolvers.ComponentVersionRepositoryResolver{
			"ocm.software/test:1.0.0": mockRes,
		},
		expectedDigest: func(_ runtime.Identity) *descriptor.Digest { return nil },
	}
	_, err := r.Resolve(t.Context(), "ocm.software/test:1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spec lookup failed")
}

func TestDiscoverer_RecursiveTargetPropagation(t *testing.T) {
	someTarget := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io/target",
	}
	d := &discoverer{
		recursive:         true,
		discoveredDigests: make(map[string]descriptor.Digest),
		targetMap:         map[string][]runtime.Typed{"parent.comp/name:1.0.0": {someTarget}},
		resolverMap:       map[string]resolvers.ComponentVersionRepositoryResolver{},
	}
	parent := &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "parent.comp/name", Version: "1.0.0"},
				},
				References: []descriptor.Reference{
					{
						ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child-ref", Version: "2.0.0"}},
						Component:   "child.comp/name",
					},
				},
			},
		},
	}
	children, err := d.Discover(t.Context(), parent)
	require.NoError(t, err)
	assert.Equal(t, []string{"child.comp/name:2.0.0"}, children)
	assert.Equal(t, []runtime.Typed{someTarget}, d.targetMap["child.comp/name:2.0.0"])
}

func TestDiscoverer_RecursiveResolverPropagation(t *testing.T) {
	parentResolver := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{},
		repos: map[string]repository.ComponentVersionRepository{},
	}
	d := &discoverer{
		recursive:         true,
		discoveredDigests: make(map[string]descriptor.Digest),
		targetMap:         map[string][]runtime.Typed{},
		resolverMap: map[string]resolvers.ComponentVersionRepositoryResolver{
			"parent.comp/name:1.0.0": parentResolver,
		},
	}
	parent := &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "parent.comp/name", Version: "1.0.0"},
				},
				References: []descriptor.Reference{
					{
						ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "child-ref", Version: "3.0.0"}},
						Component:   "child.comp/name",
					},
				},
			},
		},
	}
	children, err := d.Discover(t.Context(), parent)
	require.NoError(t, err)
	assert.Equal(t, []string{"child.comp/name:3.0.0"}, children)
	assert.Equal(t, parentResolver, d.resolverMap["child.comp/name:3.0.0"])
}

func TestDiscoverer_ConflictingResolversForSameChild(t *testing.T) {
	// Scenario from PR review r2990953106:
	//
	//   A <-- root (resolver: resolverA)
	//   B <-- root (resolver: resolverB)
	//   A references D as a child
	//   B references D as a child
	//
	// Whichever parent discovers D first would win under first-writer-wins,
	// making resolution non-deterministic. We now fail hard instead.
	resolverA := &mockCVRepoResolver{specs: map[string]runtime.Typed{}, repos: map[string]repository.ComponentVersionRepository{}}
	resolverB := &mockCVRepoResolver{specs: map[string]runtime.Typed{}, repos: map[string]repository.ComponentVersionRepository{}}

	childKey := "shared.comp/d:1.0.0"

	// Simulate: A already discovered D first and assigned resolverA.
	d := &discoverer{
		recursive:         true,
		discoveredDigests: make(map[string]descriptor.Digest),
		targetMap:         map[string][]runtime.Typed{},
		resolverMap: map[string]resolvers.ComponentVersionRepositoryResolver{
			"parent.comp/b:1.0.0": resolverB,
			childKey:              resolverA, // already claimed by A
		},
	}

	// Now B tries to discover D — it has a different resolver, which must fail.
	parentB := &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "parent.comp/b", Version: "1.0.0"},
				},
				References: []descriptor.Reference{
					{
						ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "d-ref", Version: "1.0.0"}},
						Component:   "shared.comp/d",
					},
				},
			},
		},
	}

	_, err := d.Discover(t.Context(), parentB)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous resolver")
	assert.Contains(t, err.Error(), childKey)
}

func TestDiscoverer_SameResolverForSameChildFromTwoParents(t *testing.T) {
	// Two parents referencing the same child with the same resolver is fine.
	sharedResolver := &mockCVRepoResolver{specs: map[string]runtime.Typed{}, repos: map[string]repository.ComponentVersionRepository{}}

	childKey := "shared.comp/d:1.0.0"

	d := &discoverer{
		recursive:         true,
		discoveredDigests: make(map[string]descriptor.Digest),
		targetMap:         map[string][]runtime.Typed{},
		resolverMap: map[string]resolvers.ComponentVersionRepositoryResolver{
			"parent.comp/b:1.0.0": sharedResolver,
			childKey:              sharedResolver, // already claimed, same resolver
		},
	}

	parentB := &discoveryValue{
		Descriptor: &descriptor.Descriptor{
			Component: descriptor.Component{
				ComponentMeta: descriptor.ComponentMeta{
					ObjectMeta: descriptor.ObjectMeta{Name: "parent.comp/b", Version: "1.0.0"},
				},
				References: []descriptor.Reference{
					{
						ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "d-ref", Version: "1.0.0"}},
						Component:   "shared.comp/d",
					},
				},
			},
		},
	}

	children, err := d.Discover(t.Context(), parentB)
	require.NoError(t, err)
	assert.Equal(t, []string{childKey}, children)
	assert.Equal(t, sharedResolver, d.resolverMap[childKey])
}

func TestMultiResolver_NoResolverForKey(t *testing.T) {
	r := &multiResolver{
		mu:             &sync.Mutex{},
		resolverMap:    map[string]resolvers.ComponentVersionRepositoryResolver{},
		expectedDigest: func(_ runtime.Identity) *descriptor.Digest { return nil },
	}
	_, err := r.Resolve(t.Context(), "ocm.software/missing:1.0.0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no resolver found")
}

func TestMultiResolver_NilRepoSpec_FallsBackToDirectLookup(t *testing.T) {
	desc := &descriptor.Descriptor{
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/test", Version: "1.0.0"},
			},
		},
	}
	mockRepo := &mockCVRepo{
		descriptors: map[string]*descriptor.Descriptor{
			"ocm.software/test:1.0.0": desc,
		},
	}
	mockRes := &mockCVRepoResolver{
		specs: map[string]runtime.Typed{
			"ocm.software/test:1.0.0": nil, // GetRepositorySpecificationForComponent returns nil
		},
		repos: map[string]repository.ComponentVersionRepository{
			"ocm.software/test:1.0.0": mockRepo,
		},
	}
	r := &multiResolver{
		mu: &sync.Mutex{},
		resolverMap: map[string]resolvers.ComponentVersionRepositoryResolver{
			"ocm.software/test:1.0.0": mockRes,
		},
		expectedDigest: func(_ runtime.Identity) *descriptor.Digest { return nil },
	}
	val, err := r.Resolve(t.Context(), "ocm.software/test:1.0.0")
	require.NoError(t, err)
	assert.Equal(t, desc, val.Descriptor)
	assert.Nil(t, val.SourceRepository, "SourceRepository should be nil when repo spec is nil")
}
