package credentials

import (
	"context"
	"fmt"

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

// Resolve attempts to resolve credentials for a given repository configuration and consumer identity.
// It converts the provided configuration to a DockerConfig and uses it to resolve credentials.
// The passed map of pre-resolved credentials is unused in this implementation, because docker configs do not require
// authentication themselves.
//
// Returns a map of credential key-value pairs (username, password, access token, refresh token as per ResolveV1DockerConfigCredentials)
func (p *OCICredentialRepository) Resolve(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, _ map[string]string) (map[string]string, error) {
	dockerConfig := credentialsv1.DockerConfig{}
	if err := p.GetCredentialRepositoryScheme().Convert(cfg, &dockerConfig); err != nil {
		return nil, fmt.Errorf("failed to resolve credentials because config could not be interpreted as docker config: %w", err)
	}
	return ResolveV1DockerConfigCredentials(ctx, dockerConfig, identity)
}

// ConsumerIdentityForConfig is not supported for Docker config files as they are
// expected to be available on the host system and don't require consumer identity mapping.
// This method always returns an error to indicate that consumer identities are not needed.
// The credential graph should in this case not lookup credentials for this repository provider.
func (p *OCICredentialRepository) ConsumerIdentityForConfig(_ context.Context, _ runtime.Typed) (runtime.Identity, error) {
	return nil, fmt.Errorf("credential consumer identities are not necessary for a docker config file and are thus not supported." +
		"If you need to use a docker config file, it needs to be available on the host system as is, so it shouldn't need to generate a consumer identity")
}
