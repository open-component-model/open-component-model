package discovery

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// fakeResolver is a ComponentVersionRepositoryResolver fake that routes each
// component identity to a fakeRepo and counts fetch calls per component key.
type fakeResolver struct {
	mu         sync.Mutex
	descs      map[ComponentKey]*descriptor.Descriptor
	calls      map[ComponentKey]int
	fetchErr   map[ComponentKey]error
	resolveErr map[ComponentKey]error
	delay      time.Duration
}

var _ resolvers.ComponentVersionRepositoryResolver = (*fakeResolver)(nil)

func newFakeResolver(descs ...*descriptor.Descriptor) *fakeResolver {
	f := &fakeResolver{
		descs:      map[ComponentKey]*descriptor.Descriptor{},
		calls:      map[ComponentKey]int{},
		fetchErr:   map[ComponentKey]error{},
		resolveErr: map[ComponentKey]error{},
	}
	for _, d := range descs {
		f.descs[ComponentKey{Name: d.Component.Name, Version: d.Component.Version}] = d
	}
	return f
}

func (f *fakeResolver) failFetch(key ComponentKey, err error) *fakeResolver {
	f.fetchErr[key] = err
	return f
}

func (f *fakeResolver) failResolve(key ComponentKey, err error) *fakeResolver {
	f.resolveErr[key] = err
	return f
}

func (f *fakeResolver) GetComponentVersionRepositoryForComponent(_ context.Context, component, version string) (repository.ComponentVersionRepository, error) {
	if err := f.resolveErr[ComponentKey{Name: component, Version: version}]; err != nil {
		return nil, err
	}
	return &fakeRepo{resolver: f}, nil
}

func (f *fakeResolver) GetComponentVersionRepositoryForSpecification(context.Context, runtime.Typed) (repository.ComponentVersionRepository, error) {
	return &fakeRepo{resolver: f}, nil
}

func (f *fakeResolver) GetRepositorySpecificationForComponent(context.Context, string, string) (runtime.Typed, error) {
	return nil, nil
}

// fakeRepo is the ComponentVersionRepository returned by fakeResolver. Only
// GetComponentVersion is exercised by Traverse.
type fakeRepo struct {
	repository.ComponentVersionRepository
	resolver *fakeResolver
}

func (r *fakeRepo) GetComponentVersion(ctx context.Context, name, version string) (*descriptor.Descriptor, error) {
	f := r.resolver
	f.mu.Lock()
	f.calls[ComponentKey{Name: name, Version: version}]++
	f.mu.Unlock()

	if f.delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.delay):
		}
	}

	if err := f.fetchErr[ComponentKey{Name: name, Version: version}]; err != nil {
		return nil, err
	}
	desc, ok := f.descs[ComponentKey{Name: name, Version: version}]
	if !ok {
		return nil, fmt.Errorf("component %s:%s not found", name, version)
	}
	return desc, nil
}

func graphKeys(g *Graph) []ComponentKey {
	keys := make([]ComponentKey, 0, len(g.Descriptors))
	for _, d := range g.Descriptors {
		keys = append(keys, ComponentKey{Name: d.Component.Name, Version: d.Component.Version})
	}
	slices.SortFunc(keys, func(a, b ComponentKey) int { return strings.Compare(a.String(), b.String()) })
	return keys
}

func TestTraverse_NilResolver(t *testing.T) {
	r := require.New(t)

	_, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, nil)
	r.ErrorContains(err, "resolver must not be nil")
}

func TestTraverse_ResolvesCompleteGraph(t *testing.T) {
	r := require.New(t)

	// root -> [a, b]; a -> c; b -> c (diamond with a duplicate reference entry)
	root := newDescriptor("root", "1.0.0", withReferences(
		newReference("a", "a", "1.0.0"),
		newReference("b", "b", "1.0.0"),
	))
	a := newDescriptor("a", "1.0.0", withReferences(newReference("c", "c", "1.0.0")))
	b := newDescriptor("b", "1.0.0", withReferences(
		newReference("c", "c", "1.0.0"),
		newReference("c-dupe", "c", "1.0.0"),
	))
	c := newDescriptor("c", "1.0.0")
	resolver := newFakeResolver(root, a, b, c)

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, resolver)
	r.NoError(err)
	r.Equal(ComponentKey{Name: "root", Version: "1.0.0"}, graph.Root)
	r.Equal([]ComponentKey{
		{Name: "a", Version: "1.0.0"},
		{Name: "b", Version: "1.0.0"},
		{Name: "c", Version: "1.0.0"},
		{Name: "root", Version: "1.0.0"},
	}, graphKeys(graph))

	// The duplicate reference entries on b remain untouched.
	for _, d := range graph.Descriptors {
		if d.Component.Name == "b" {
			r.Len(d.Component.References, 2, "duplicate reference entries must remain untouched")
		}
	}
}

func TestTraverse_ResolvesEachComponentExactlyOnce(t *testing.T) {
	r := require.New(t)

	// Deep diamond: every level references level 3 twice; without resolve-once,
	// the shared resolver would see duplicate calls per component.
	root := newDescriptor("root", "1.0.0", withReferences(
		newReference("a", "a", "1.0.0"),
		newReference("b", "b", "1.0.0"),
	))
	a := newDescriptor("a", "1.0.0", withReferences(newReference("c", "c", "1.0.0")))
	// concurrent resolution is slowed down to force overlapping schedules
	b := newDescriptor("b", "1.0.0", withReferences(newReference("c", "c", "1.0.0")))
	c := newDescriptor("c", "1.0.0")
	resolver := newFakeResolver(root, a, b, c)
	resolver.delay = 20 * time.Millisecond

	_, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, resolver)
	r.NoError(err)
	for key, count := range resolver.calls {
		r.Equal(1, count, "component %s resolved more than once", key)
	}
	r.Equal(4, len(resolver.calls), "deep matches behind unmatched-by-name ancestors must be resolved")
}

func TestTraverse_DistinctVersionsOfOneComponent(t *testing.T) {
	r := require.New(t)

	// Two distinct versions of the same component name must both be resolved.
	root := newDescriptor("root", "1.0.0", withReferences(
		newReference("a-old", "a", "1.0.0"),
		newReference("a-new", "a", "2.0.0"),
	))
	aOld := newDescriptor("a", "1.0.0")
	aNew := newDescriptor("a", "2.0.0")
	resolver := newFakeResolver(root, aOld, aNew)

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, resolver)
	r.NoError(err)
	r.Equal([]ComponentKey{
		{Name: "a", Version: "1.0.0"},
		{Name: "a", Version: "2.0.0"},
		{Name: "root", Version: "1.0.0"},
	}, graphKeys(graph))
}

func TestTraverse_DeepMatchesBehindUnmatchedAncestors(t *testing.T) {
	r := require.New(t)

	// The target lives two levels deep; the intermediate level itself
	// contributes nothing filter-relevant but must still be traversed.
	root := newDescriptor("root", "1.0.0", withReferences(newReference("mid", "mid", "1.0.0")))
	mid := newDescriptor("mid", "1.0.0", withReferences(newReference("leaf", "leaf", "1.0.0")))
	leaf := newDescriptor("leaf", "1.0.0")
	resolver := newFakeResolver(root, mid, leaf)

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, resolver)
	r.NoError(err)
	r.Len(graph.Descriptors, 3)
}

func TestTraverse_RootResolutionFailure(t *testing.T) {
	r := require.New(t)

	resolver := newFakeResolver().failFetch(ComponentKey{Name: "root", Version: "1.0.0"}, fmt.Errorf("boom"))

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, resolver)
	r.Nil(graph)
	r.ErrorContains(err, "boom")
	r.ErrorContains(err, "name=root,version=1.0.0")
	// the wrapped cause is preserved
	r.ErrorContains(err, "failed to resolve component")
}

func TestTraverse_RepositorySelectionFailure(t *testing.T) {
	r := require.New(t)

	// Repository selection (not the fetch) fails: the wrapped cause is preserved
	// and no graph is returned.
	resolver := newFakeResolver().failResolve(ComponentKey{Name: "root", Version: "1.0.0"}, fmt.Errorf("no repository"))

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, resolver)
	r.Nil(graph)
	r.ErrorContains(err, "no repository")
	r.ErrorContains(err, "failed to resolve repository for component")
	r.ErrorContains(err, "name=root,version=1.0.0")
}

func TestTraverse_MissingSiblingFailsEntireTraversal(t *testing.T) {
	r := require.New(t)

	// One reference target resolves, the sibling is missing: the complete
	// traversal must fail regardless of which one finishes first.
	root := newDescriptor("root", "1.0.0", withReferences(
		newReference("ok", "ok", "1.0.0"),
		newReference("missing", "missing", "1.0.0"),
	))
	ok := newDescriptor("ok", "1.0.0")
	resolver := newFakeResolver(root, ok)

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, resolver)
	r.Nil(graph)
	r.ErrorContains(err, "missing:1.0.0 not found")
}

func TestTraverse_Cancellation(t *testing.T) {
	r := require.New(t)

	root := newDescriptor("root", "1.0.0", withReferences(newReference("a", "a", "1.0.0")))
	a := newDescriptor("a", "1.0.0")
	resolver := newFakeResolver(root, a)
	resolver.delay = time.Second

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	graph, err := Traverse(ctx, ComponentKey{Name: "root", Version: "1.0.0"}, resolver)
	r.Nil(graph)
	r.ErrorIs(err, context.Canceled)
}

func TestTraverse_NilDescriptorIsAFailure(t *testing.T) {
	r := require.New(t)

	// A resolver returning a repo that yields a nil descriptor is a failure.
	resolver := newFakeResolver()
	resolver.descs[ComponentKey{Name: "root", Version: "1.0.0"}] = nil

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, resolver)
	r.Nil(graph)
	r.ErrorContains(err, "resolved descriptor is nil")
}
