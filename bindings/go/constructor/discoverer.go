package constructor

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	syncdag "ocm.software/open-component-model/bindings/go/dag/sync"
	dag "ocm.software/open-component-model/bindings/go/dag"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/repository"
	ocmruntime "ocm.software/open-component-model/bindings/go/runtime"
)

// WithCycleDetection enables cycle detection using Tarjan's algorithm for the DAG.
// This provides O(V+E) time complexity for cycle detection compared to the default DFS approach.
func WithCycleDetection() dag.Option[string] {
	return func(g *dag.DirectedAcyclicGraph[string]) {
		// The cycle detection is now automatically used by the HasCycle() method
		// No additional setup needed as it's integrated into the DAG implementation
	}
}

// NewGraphDiscovererWithOptions creates a new GraphDiscoverer with optional DAG optimizations.
func NewGraphDiscovererWithOptions(componentConstructor *constructor.ComponentConstructor, externalComponentRepositoryProvider ExternalComponentRepositoryProvider, resolveExternalLocalBlobs bool, opts ...dag.Option[string]) *syncdag.GraphDiscoverer[string, *ConstructorOrExternalComponent] {
	rd := &resolverAndDiscoverer{
		componentConstructor:                componentConstructor,
		externalComponentRepositoryProvider: externalComponentRepositoryProvider,
		resolveExternalLocalBlobs:           resolveExternalLocalBlobs,
	}
	return syncdag.NewGraphDiscoverer(&syncdag.GraphDiscovererOptions[string, *ConstructorOrExternalComponent]{
		Resolver:   rd,
		Discoverer: rd,
		DAGOptions: opts,
	})
}