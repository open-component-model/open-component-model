package resource

import (
	"context"
	"fmt"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ repository.AttestationDiscoverer = (*ResourceRepository)(nil)

// DiscoverAttestations returns the attestations attached to the OCI artifact backing the
// resource, authenticating against the registry with the given credentials.
func (p *ResourceRepository) DiscoverAttestations(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed, opts ...repository.AttestationOption) ([]repository.Attestation, error) {
	repo, _, err := p.resolveRepository(resource, credentials)
	if err != nil {
		return nil, err
	}
	atts, err := repo.DiscoverAttestations(ctx, resource, opts...)
	if err != nil {
		return nil, fmt.Errorf("discovering attestations for resource %q failed: %w", resource.ToIdentity(), err)
	}
	return atts, nil
}
