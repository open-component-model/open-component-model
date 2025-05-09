package oci

import (
	"context"

	"ocm.software/open-component-model/bindings/go/credentials"
	ocicredentials "ocm.software/open-component-model/bindings/go/oci/credentials"
	ocicredentialsspec "ocm.software/open-component-model/bindings/go/oci/spec/credentials"
	ocicredentialsspecv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/credentialrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Register(registry *credentialrepository.RepositoryRegistry) error {
	scheme := runtime.NewScheme()
	scheme.MustRegisterWithAlias(&ocicredentialsspecv1.DockerConfig{}, ocicredentialsspec.CredentialRepositoryConfigType)
	return credentialrepository.RegisterInternalCredentialRepositoryPlugin(
		scheme,
		registry,
		&Plugin{base: ocicredentials.OCICredentialRepository{}},
		&ocicredentialsspecv1.DockerConfig{},
		[]runtime.Type{credentials.AnyConsumerIdentityType},
	)
}

type Plugin struct {
	contracts.EmptyBasePlugin
	base ocicredentials.OCICredentialRepository
}

var _ v1.CredentialRepositoryPluginContract[*ocicredentialsspecv1.DockerConfig] = &Plugin{}

func (p *Plugin) ConsumerIdentityForConfig(ctx context.Context, request v1.ConsumerIdentityForConfigRequest[*ocicredentialsspecv1.DockerConfig]) (runtime.Identity, error) {
	return p.base.ConsumerIdentityForConfig(ctx, request.Config)
}

func (p *Plugin) Resolve(ctx context.Context, request v1.ResolveRequest[*ocicredentialsspecv1.DockerConfig], credentials map[string]string) (map[string]string, error) {
	return p.base.Resolve(ctx, request.Config, request.Identity, credentials)
}
