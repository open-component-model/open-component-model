package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	contractsv1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/componentversionrepository"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/plugin/builtin/location"
)

const Creator = "OCI Repository TypeToUntypedPlugin"

func Register(registry *componentversionrepository.RepositoryRegistry) error {
	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	return componentversionrepository.RegisterInternalComponentVersionRepositoryPlugin(
		scheme,
		registry,
		&Plugin{scheme: scheme, memory: inmemory.New()},
		&ctfv1.Repository{},
	)
}

type Plugin struct {
	contracts.EmptyBasePlugin
	scheme *runtime.Scheme
	memory cache.OCIDescriptorCache
}

func (p *Plugin) GetIdentity(_ context.Context, _ contractsv1.GetIdentityRequest[*ctfv1.Repository]) (runtime.Identity, error) {
	return nil, fmt.Errorf("not implemented because ctfs do not need consumer identity based credentials")
}

func (p *Plugin) ListComponentVersions(ctx context.Context, request contractsv1.ListComponentVersionsRequest[*ctfv1.Repository], credentials map[string]string) ([]string, error) {
	repo, err := p.createRepository(request.Repository)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo.ListComponentVersions(ctx, request.Name)
}

func (p *Plugin) GetComponentVersion(ctx context.Context, request contractsv1.GetComponentVersionRequest[*ctfv1.Repository], _ map[string]string) (*descriptor.Descriptor, error) {
	repo, err := p.createRepository(request.Repository)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo.GetComponentVersion(ctx, request.Name, request.Version)
}

func (p *Plugin) AddComponentVersion(ctx context.Context, request contractsv1.PostComponentVersionRequest[*ctfv1.Repository], _ map[string]string) error {
	repo, err := p.createRepository(request.Repository)
	if err != nil {
		return fmt.Errorf("error creating repository: %w", err)
	}
	desc, err := descriptor.ConvertFromV2(request.Descriptor)
	if err != nil {
		return fmt.Errorf("error converting descriptor: %w", err)
	}
	return repo.AddComponentVersion(ctx, desc)
}

func (p *Plugin) AddLocalResource(ctx context.Context, request contractsv1.PostLocalResourceRequest[*ctfv1.Repository], _ map[string]string) (*descriptor.Resource, error) {
	repo, err := p.createRepository(request.Repository)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	resource := descriptor.ConvertFromV2Resources([]v2.Resource{*request.Resource})[0]

	b, err := location.Read(request.ResourceLocation)
	if err != nil {
		return nil, fmt.Errorf("error reading blob from location: %w", err)
	}

	newRes, err := repo.AddLocalResource(ctx, request.Name, request.Version, &resource, b)
	if err != nil {
		return nil, fmt.Errorf("error adding local resource: %w", err)
	}
	return newRes, nil
}

func (p *Plugin) GetLocalResource(ctx context.Context, request contractsv1.GetLocalResourceRequest[*ctfv1.Repository], _ map[string]string) (contractsv1.GetLocalResourceResponse, error) {
	repo, err := p.createRepository(request.Repository)
	if err != nil {
		return contractsv1.GetLocalResourceResponse{}, fmt.Errorf("error creating repository: %w", err)
	}
	b, res, err := repo.GetLocalResource(ctx, request.Name, request.Version, request.Identity)
	if err != nil {
		return contractsv1.GetLocalResourceResponse{}, fmt.Errorf("error getting local resource: %w", err)
	}

	path := filepath.Join(os.TempDir(), fmt.Sprintf("ocm-local-resource-%d", res.ToIdentity().CanonicalHashV1()))
	tmp, err := os.Create(path)
	if err != nil {
		return contractsv1.GetLocalResourceResponse{}, fmt.Errorf("error creating buffer file: %w", err)
	}
	_ = tmp.Close() // Ensure the file is closed after creation

	loc := types.Location{
		LocationType: types.LocationTypeLocalFile,
		Value:        path,
	}

	if err := location.Write(loc, b); err != nil {
		return contractsv1.GetLocalResourceResponse{}, fmt.Errorf("error writing blob to location: %w", err)
	}

	return contractsv1.GetLocalResourceResponse{Location: loc}, nil
}

var _ contractsv1.ReadWriteOCMRepositoryPluginContract[*ctfv1.Repository] = (*Plugin)(nil)

func (p *Plugin) createRepository(spec *ctfv1.Repository) (oci.ComponentVersionRepository, error) {
	archive, err := ctf.OpenCTFFromOSPath(spec.Path, spec.AccessMode.ToAccessBitmask())
	if err != nil {
		return nil, fmt.Errorf("error opening CTF archive: %w", err)
	}
	repo, err := oci.NewRepository(
		ocictf.WithCTF(ocictf.NewFromCTF(archive)),
		oci.WithScheme(p.scheme),
		oci.WithCreator(Creator),
		oci.WithOCIDescriptorCache(p.memory),
	)
	return repo, err
}
