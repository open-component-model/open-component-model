package credentials

import (
	"context"
	"fmt"

	ocicredentials "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	credentialsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type OCICredentialRepository struct{}

func (p *OCICredentialRepository) Resolve(ctx context.Context, cfg runtime.Typed, identity runtime.Identity, _ map[string]string) (map[string]string, error) {
	dockerConfig := credentialsv1.DockerConfig{}
	if err := ocicredentials.Scheme.Convert(cfg, &dockerConfig); err != nil {
		return nil, fmt.Errorf("failed to resolve credentials because config could not be interpreted as docker config: %v", err)
	}
	return ResolveV1DockerConfigCredentials(ctx, dockerConfig, identity)
}

func (p *OCICredentialRepository) SupportedRepositoryConfigTypes() []runtime.Type {
	return []runtime.Type{
		ocicredentials.CredentialRepositoryConfigType,
	}
}

func (p *OCICredentialRepository) ConsumerIdentityForConfig(_ context.Context, _ runtime.Typed) (runtime.Identity, error) {
	return nil, fmt.Errorf("credential consumer identities are not necessary for a docker config file and are thus not supported." +
		"If you need to use a docker config file, it needs to be available on the host system as is, so it shouldn't need to generate a consumer identity.")
}
