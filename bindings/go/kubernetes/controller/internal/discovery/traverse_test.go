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
)

// fakeGetter is a component repository fake counting calls per component key.
type fakeGetter struct {
	mu      sync.Mutex
	descs   map[ComponentKey]*descriptor.Descriptor
	calls   map[ComponentKey]int
	failErr map[ComponentKey]error
	delay   time.Duration
}

func newFakeGetter(descs ...*descriptor.Descriptor) *fakeGetter {
	f := &fakeGetter{
		descs:   map[ComponentKey]*descriptor.Descriptor{},
		calls:   map[ComponentKey]int{},
		failErr: map[ComponentKey]error{},
	}
	for _, d := range descs {
		f.descs[ComponentKey{Name: d.Component.Name, Version: d.Component.Version}] = d
	}
	return f
}

func (f *fakeGetter) fail(key ComponentKey, err error) *fakeGetter {
	f.failErr[key] = err
	return f
}

func (f *fakeGetter) GetComponentVersion(ctx context.Context, name, version string) (*descriptor.Descriptor, error) {
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

	if err := f.failErr[ComponentKey{Name: name, Version: version}]; err != nil {
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

func TestTraverse_NilGetter(t *testing.T) {
	r := require.New(t)

	_, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, nil)
	r.ErrorContains(err, "component getter must not be nil")
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
	getter := newFakeGetter(root, a, b, c)

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, getter)
	r.NoError(err)
	r.Equal(ComponentKey{Name: "root", Version: "1.0.0"}, graph.Root)
	r.Equal([]ComponentKey{
		{Name: "a", Version: "1.0.0"},
		{Name: "b", Version: "1.0.0"},
		{Name: "c", Version: "1.0.0"},
		{Name: "root", Version: "1.0.0"},
	}, graphKeys(graph))
}

func TestTraverse_ResolvesEachComponentExactlyOnce(t *testing.T) {
	r := require.New(t)

	// Deep diamond: every level references level 3 twice; without resolve-once,
	// the shared getter would see duplicate calls per component.
	root := newDescriptor("root", "1.0.0", withReferences(
		newReference("a", "a", "1.0.0"),
		newReference("b", "b", "1.0.0"),
	))
	a := newDescriptor("a", "1.0.0", withReferences(newReference("c", "c", "1.0.0")))
	// concurrent resolution is slowed down to force overlapping schedules
	b := newDescriptor("b", "1.0.0", withReferences(newReference("c", "c", "1.0.0")))
	c := newDescriptor("c", "1.0.0")
	getter := newFakeGetter(root, a, b, c)
	getter.delay = 20 * time.Millisecond

	_, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, getter)
	r.NoError(err)
	for key, count := range getter.calls {
		r.Equal(1, count, "component %s resolved more than once", key)
	}
	r.Equal(4, len(getter.calls), "deep matches behind unmatched-by-name ancestors must be resolved")
}

func TestTraverse_DeepMatchesBehindUnmatchedAncestors(t *testing.T) {
	r := require.New(t)

	// The target lives two levels deep; the intermediate level itself
	// contributes nothing filter-relevant but must still be traversed.
	root := newDescriptor("root", "1.0.0", withReferences(newReference("mid", "mid", "1.0.0")))
	mid := newDescriptor("mid", "1.0.0", withReferences(newReference("leaf", "leaf", "1.0.0")))
	leaf := newDescriptor("leaf", "1.0.0")
	getter := newFakeGetter(root, mid, leaf)

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, getter)
	r.NoError(err)
	r.Len(graph.Descriptors, 3)
}

func TestTraverse_RootResolutionFailure(t *testing.T) {
	r := require.New(t)

	getter := newFakeGetter().fail(ComponentKey{Name: "root", Version: "1.0.0"}, fmt.Errorf("boom"))

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, getter)
	r.Nil(graph)
	r.ErrorContains(err, "boom")
	r.ErrorContains(err, "root:1.0.0")
	// the wrapped cause is preserved
	r.ErrorContains(err, "failed to resolve component")
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
	getter := newFakeGetter(root, ok)

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"}, getter)
	r.Nil(graph)
	r.ErrorContains(err, "missing:1.0.0 not found")
}

func TestTraverse_Cancellation(t *testing.T) {
	r := require.New(t)

	root := newDescriptor("root", "1.0.0", withReferences(newReference("a", "a", "1.0.0")))
	a := newDescriptor("a", "1.0.0")
	getter := newFakeGetter(root, a)
	getter.delay = time.Second

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	graph, err := Traverse(ctx, ComponentKey{Name: "root", Version: "1.0.0"}, getter)
	r.Nil(graph)
	r.ErrorIs(err, context.Canceled)
}

func TestTraverse_NilDescriptorIsAFailure(t *testing.T) {
	r := require.New(t)

	graph, err := Traverse(t.Context(), ComponentKey{Name: "root", Version: "1.0.0"},
		ComponentGetterFunc(func(ctx context.Context, name, version string) (*descriptor.Descriptor, error) {
			return nil, nil
		}))
	r.Nil(graph)
	r.ErrorContains(err, "resolved descriptor is nil")
}
