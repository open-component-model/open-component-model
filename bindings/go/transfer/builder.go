package transfer

import (
	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/transfer/internal"
	"ocm.software/open-component-model/bindings/go/transform/graph/builder"
)

// NewDefaultBuilder creates a builder.Builder pre-configured with all standard OCI, CTF, and Helm transformers.
// It accepts the repository provider, resource repository, and credential resolver interfaces
// that are needed by the transformers to interact with repositories.
func NewDefaultBuilder(
	repoProvider repository.ComponentVersionRepositoryProvider,
	resourceRepo repository.ResourceRepository,
	credentialProvider credentials.Resolver,
) *builder.Builder {
	return internal.NewDefaultBuilder(repoProvider, resourceRepo, credentialProvider)
}
