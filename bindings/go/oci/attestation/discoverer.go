package attestation

import (
	"context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// SBOMDiscoverer may be implemented by a resource repository that can retrieve
// SBOMs for a given resource.
type SBOMDiscoverer interface {
	DiscoverSBOM(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed, opts ...Option) ([]SBOM, error)
}
