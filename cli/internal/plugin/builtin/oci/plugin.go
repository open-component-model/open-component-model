package plugin

import (
	"context"
	"fmt"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob"
	ocmrepository "ocm.software/open-component-model/bindings/go/componentversionrepository"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const Creator = "OCI Repository TypeToUntypedPlugin"

func Register(registry *componentversionrepository.RepositoryRegistry) error {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	return componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		scheme,
		registry,
		&Plugin{scheme: scheme, manifestCache: inmemory.New(), layerCache: inmemory.New()},
		&ociv1.Repository{},
	)
}

type Plugin struct {
	contracts.EmptyBasePlugin
	scheme        *runtime.Scheme
	manifestCache cache.OCIDescriptorCache
	layerCache    cache.OCIDescriptorCache
}

func (p *Plugin) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	ociRepoSpec, ok := repositorySpecification.(*ociv1.Repository)
	if !ok {
		return nil, fmt.Errorf("invalid repository specification: %T", repositorySpecification)
	}

	identity, err := runtime.ParseURLToIdentity(ociRepoSpec.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL to identity: %w", err)
	}
	identity.SetType(runtime.NewVersionedType(ociv1.Type, ociv1.Version))

	return identity, nil
}

func (p *Plugin) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (ocmrepository.ComponentVersionRepository, error) {
	ociRepoSpec, ok := repositorySpecification.(*ociv1.Repository)
	if !ok {
		return nil, fmt.Errorf("invalid repository specification: %T", repositorySpecification)
	}

	repo, err := p.createRepository(ociRepoSpec, credentials)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}

	return &wrapper{repo: repo}, nil
}

var (
	_ ocmrepository.ComponentVersionRepositoryProvider = (*Plugin)(nil)
	_ ocmrepository.ComponentVersionRepository         = (*wrapper)(nil)
)

// wrapper wraps a repo into returning the component version repository ComponentVersionRepository.
// That's because the plugin interface uses ReadyOnlyBlob while the oci.ComponentVersionRepository uses LocalBlob
// specific to OCI and CTF.
type wrapper struct {
	repo oci.ComponentVersionRepository
}

func (w *wrapper) AddComponentVersion(ctx context.Context, descriptor *descriptor.Descriptor) error {
	return w.repo.AddComponentVersion(ctx, descriptor)
}

func (w *wrapper) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	return w.repo.GetComponentVersion(ctx, component, version)
}

func (w *wrapper) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	return w.repo.ListComponentVersions(ctx, component)
}

func (w *wrapper) AddLocalResource(ctx context.Context, component, version string, res *descriptor.Resource, content blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return w.repo.AddLocalResource(ctx, component, version, res, content)
}

func (w *wrapper) GetLocalResource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return w.repo.GetLocalResource(ctx, component, version, identity)
}

func (w *wrapper) AddLocalSource(ctx context.Context, component, version string, res *descriptor.Source, content blob.ReadOnlyBlob) (*descriptor.Source, error) {
	return w.repo.AddLocalSource(ctx, component, version, res, content)
}

func (w *wrapper) GetLocalSource(ctx context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Source, error) {
	return w.repo.GetLocalSource(ctx, component, version, identity)
}

// TODO(jakobmoellerdev): add identity mapping function from OCI package here as soon as we have the conversion function
func (p *Plugin) createRepository(spec *ociv1.Repository, credentials map[string]string) (oci.ComponentVersionRepository, error) {
	url, err := runtime.ParseURLAndAllowNoScheme(spec.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", spec.BaseUrl, err)
	}
	urlString := url.Host + url.Path

	urlResolver, err := urlresolver.New(urlresolver.WithBaseURL(urlString))
	if err != nil {
		return nil, fmt.Errorf("error creating URL resolver: %w", err)
	}

	urlResolver.SetClient(&auth.Client{
		Client: retry.DefaultClient,
		Header: map[string][]string{
			"User-Agent": {Creator},
		},
		Credential: auth.StaticCredential(url.Host, clientCredentials(credentials)),
	})
	repo, err := oci.NewRepository(
		oci.WithResolver(urlResolver),
		oci.WithScheme(p.scheme),
		oci.WithCreator(Creator),
		oci.WithManifestCache(p.manifestCache),
		oci.WithLayerCache(p.layerCache),
	)
	return repo, err
}

func clientCredentials(credentials map[string]string) auth.Credential {
	cred := auth.Credential{}
	if username, ok := credentials["username"]; ok {
		cred.Username = username
	}
	if password, ok := credentials["password"]; ok {
		cred.Password = password
	}
	if refreshToken, ok := credentials["refresh_token"]; ok {
		cred.RefreshToken = refreshToken
	}
	if accessToken, ok := credentials["access_token"]; ok {
		cred.AccessToken = accessToken
	}
	return cred
}
