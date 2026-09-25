package resource

import (
	"context"
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ repository.SBOMDiscoverer = (*ResourceRepository)(nil)

// DiscoverSBOM returns the SBOM attestations attached to the OCI artifact backing the
// resource, authenticating against the registry with the given credentials.
func (p *ResourceRepository) DiscoverSBOM(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed, opts ...repository.SBOMOption) ([]repository.SBOM, error) {
	repo, err := p.resolveOCIImageRepo(resource, credentials)
	if err != nil {
		return nil, err
	}
	sboms, err := repo.DiscoverSBOM(ctx, resource, opts...)
	if err != nil {
		return nil, fmt.Errorf("discovering sbom for resource %q failed: %w", resource.ToIdentity(), err)
	}
	return sboms, nil
}
