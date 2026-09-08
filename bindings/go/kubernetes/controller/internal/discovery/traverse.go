package discovery

import (
	"context"
	"fmt"
	"strings"

	"ocm.software/open-component-model/bindings/go/dag"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// ComponentGetter resolves the descriptor of a single component version. It is
// the only repository dependency of Traverse.
type ComponentGetter interface {
	GetComponentVersion(ctx context.Context, name, version string) (*descriptor.Descriptor, error)
}

// ComponentGetterFunc adapts a function to ComponentGetter.
type ComponentGetterFunc func(ctx context.Context, name, version string) (*descriptor.Descriptor, error)

// GetComponentVersion calls the underlying function.
func (f ComponentGetterFunc) GetComponentVersion(ctx context.Context, name, version string) (*descriptor.Descriptor, error) {
	return f(ctx, name, version)
}

// traversalKey encodes a (component name, version) pair as an ordered DAG key.
// NUL can neither appear in component names nor versions, so the encoding is
// unambiguous.
type traversalKey string

func keyFor(key ComponentKey) traversalKey {
	return traversalKey(key.Name + "\x00" + key.Version)
}

func (k traversalKey) identity() ComponentKey {
	name, version, _ := strings.Cut(string(k), "\x00")
	return ComponentKey{Name: name, Version: version}
}

// Traverse resolves the complete transitive component graph reachable from
// root through component references. Every referenced component version is
// resolved exactly once via get; duplicate reference entries to the same
// target are deduplicated while the descriptor reference entries themselves
// remain untouched.
//
// The traversal is fail-fast: the first resolution failure cancels the
// remaining traversal and is returned wrapped with the failing component
// identity. The graph is only consumed after the entire traversal succeeded;
// on error no partial graph is returned.
//
// Cycle detection is intentionally not implemented here; it is handled by the
// shared DAG backlog issue (open-component-model/ocm-project#705).
func Traverse(ctx context.Context, root ComponentKey, get ComponentGetter) (*Graph, error) {
	if get == nil {
		return nil, fmt.Errorf("component getter must not be nil")
	}

	discoverer := syncdag.NewGraphDiscoverer(&syncdag.GraphDiscovererOptions[traversalKey, *descriptor.Descriptor]{
		Roots: []traversalKey{keyFor(root)},
		Resolver: syncdag.ResolverFunc[traversalKey, *descriptor.Descriptor](
			func(ctx context.Context, key traversalKey) (*descriptor.Descriptor, error) {
				id := key.identity()
				desc, err := get.GetComponentVersion(ctx, id.Name, id.Version)
				if err != nil {
					return nil, fmt.Errorf("failed to resolve component %s: %w", id, err)
				}
				if desc == nil {
					return nil, fmt.Errorf("failed to resolve component %s: resolved descriptor is nil", id)
				}
				return desc, nil
			}),
		Discoverer: syncdag.DiscovererFunc[traversalKey, *descriptor.Descriptor](
			func(_ context.Context, parent *descriptor.Descriptor) ([]traversalKey, error) {
				seen := make(map[traversalKey]struct{}, len(parent.Component.References))
				neighbors := make([]traversalKey, 0, len(parent.Component.References))
				for i := range parent.Component.References {
					ref := &parent.Component.References[i]
					neighbor := keyFor(ComponentKey{Name: ref.Component, Version: ref.Version})
					if _, ok := seen[neighbor]; ok {
						continue
					}
					seen[neighbor] = struct{}{}
					neighbors = append(neighbors, neighbor)
				}
				return neighbors, nil
			}),
	})

	if err := discoverer.Discover(ctx); err != nil {
		return nil, err
	}

	// Consume the discovered vertices only after the entire traversal succeeded.
	graph := &Graph{Root: root}
	if err := discoverer.Graph().WithReadLock(func(d *dag.DirectedAcyclicGraph[traversalKey]) error {
		descriptors := make([]*descriptor.Descriptor, 0, len(d.Vertices))
		for key, vertex := range d.Vertices {
			desc, ok := vertex.Attributes[syncdag.AttributeValue].(*descriptor.Descriptor)
			if !ok || desc == nil {
				return fmt.Errorf("discovered vertex %v has no resolved descriptor", key.identity())
			}
			descriptors = append(descriptors, desc)
		}
		graph.Descriptors = descriptors
		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to collect discovered graph: %w", err)
	}

	return graph, nil
}
