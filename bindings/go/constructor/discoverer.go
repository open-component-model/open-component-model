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