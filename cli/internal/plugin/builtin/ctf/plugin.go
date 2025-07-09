package plugin

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	ocmrepository "ocm.software/open-component-model/bindings/go/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const Creator = "CTF Repository"

func Register(registry *componentversionrepository.RepositoryRegistry) error {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	return componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		scheme,
		registry,
		&Plugin{scheme: scheme, manifestCache: inmemory.New(), layerCache: inmemory.New()},
		&ctfv1.Repository{},
	)
}

type Plugin struct {
	contracts.EmptyBasePlugin
	scheme        *runtime.Scheme
	manifestCache cache.OCIDescriptorCache
	layerCache    cache.OCIDescriptorCache
}

func (p *Plugin) GetComponentVersionRepositoryCredentialConsumerIdentity(_ context.Context, _ runtime.Typed) (runtime.Identity, error) {
	return nil, fmt.Errorf("not implemented because ctfs do not need consumer identity based credentials")
}

func (p *Plugin) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (ocmrepository.ComponentVersionRepository, error) {
	ctfRepoSpec, ok := repositorySpecification.(*ctfv1.Repository)
	if !ok {
		return nil, fmt.Errorf("invalid repository specification: %T", repositorySpecification)
	}

	repo, err := p.createRepository(ctfRepoSpec)
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

func (p *Plugin) createRepository(spec *ctfv1.Repository) (oci.ComponentVersionRepository, error) {
	archive, err := ctf.OpenCTFFromOSPath(spec.Path, spec.AccessMode.ToAccessBitmask())
	if err != nil {
		return nil, fmt.Errorf("error opening CTF archive: %w", err)
	}
	repo, err := oci.NewRepository(
		ocictf.WithCTF(ocictf.NewFromCTF(archive)),
		oci.WithCreator(Creator),
		oci.WithManifestCache(p.manifestCache),
		oci.WithLayerCache(p.layerCache),
	)
	return repo, err
}
