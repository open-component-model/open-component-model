package plugin

import (
	"context"
	"fmt"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ComponentVersionRepositoryPlugin struct {
	contracts.EmptyBasePlugin
	manifests        cache.OCIDescriptorCache
	layers           cache.OCIDescriptorCache
	filesystemConfig *v1alpha1.Config
}

func (p *ComponentVersionRepositoryPlugin) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
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

func (p *ComponentVersionRepositoryPlugin) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
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
	_ repository.ComponentVersionRepositoryProvider = (*ComponentVersionRepositoryPlugin)(nil)
	_ repository.ComponentVersionRepository         = (*wrapper)(nil)
)

// wrapper wraps a repo into returning the component version repository ComponentVersionRepositoryPlugin.
// That's because the plugin interface uses ReadyOnlyBlob while the oci.ComponentVersionRepositoryPlugin uses LocalBlob
// specific to OCI and CTF.
type wrapper struct {
	repo repository.ComponentVersionRepository
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
func (p *ComponentVersionRepositoryPlugin) createRepository(spec *ociv1.Repository, credentials map[string]string) (repository.ComponentVersionRepository, error) {
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
	tempDir := ""
	if p.filesystemConfig != nil {
		tempDir = p.filesystemConfig.TempFolder
	}
	options := []oci.RepositoryOption{
		oci.WithResolver(urlResolver),
		oci.WithCreator(Creator),
		oci.WithManifestCache(p.manifests),
		oci.WithLayerCache(p.layers),
		oci.WithTempDir(tempDir),
	}

	repo, err := oci.NewRepository(options...)
	return repo, err
}
