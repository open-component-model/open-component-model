package credentials

import (
	"context"
	"fmt"
	"log/slog"

	ocicredentials "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// OCICredentialRepository implements the RepositoryPlugin Credential Graph interface to handle OCI/Docker credential
// configurations. It provides functionality to:
// - Resolve credentials from Docker config files or inline configurations
// - Support Docker credential repository configuration types
// - Map repository configurations to consumer identities
//
// The repository supports various credential types including:
// - Username/password authentication
// - Token-based authentication (access tokens and refresh tokens)
type OCICredentialRepository struct{}

func (p *OCICredentialRepository) GetCredentialRepositoryScheme() *runtime.Scheme {
	return ocicredentials.Scheme
}

// Resolve resolves credentials and returns them as typed *credentialsv1.OCICredentials.
// The credentials parameter is unused: docker configs are read from the host and do not require
// authentication themselves.
func (p *OCICredentialRepository) Resolve(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, _ runtime.Typed) (runtime.Typed, error) {
	dockerConfig := credentialsv1.DockerConfig{}
	if err := p.GetCredentialRepositoryScheme().Convert(cfg, &dockerConfig); err != nil {
		return nil, fmt.Errorf("failed to resolve credentials because config could not be interpreted as docker config: %w", err)
	}
	resolved, err := ResolveV1DockerConfigCredentials(ctx, dockerConfig, identity)
	if err != nil {
		return nil, err
	}
	if resolved == nil {
		slog.DebugContext(ctx, "no credentials resolved for config and identity")
		return nil, nil
	}
	return resolved, nil
}

// ConsumerIdentityForConfig is not supported for Docker config files as they are
// expected to be available on the host system and don't require consumer identity mapping.
// This method always returns an error to indicate that consumer identities are not needed.
// The credential graph should in this case not lookup credentials for this repository provider.
func (p *OCICredentialRepository) ConsumerIdentityForConfig(_ context.Context, _ runtime.Typed) (runtime.Identity, error) {
	return nil, fmt.Errorf("credential consumer identities are not necessary for a docker config file and are thus not supported." +
		"If you need to use a docker config file, it needs to be available on the host system as is, so it shouldn't need to generate a consumer identity")
}
