package plugin

import (
	"context"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
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

	b, err := readBlobFromLocation(request.ResourceLocation)
	if err != nil {
		return nil, fmt.Errorf("error reading blob from location: %w", err)
	}

	newRes, err := repo.AddLocalResource(ctx, request.Name, request.Version, &resource, b)
	if err != nil {
		return nil, fmt.Errorf("error adding local resource: %w", err)
	}
	return newRes, nil
}

func (p *Plugin) GetLocalResource(ctx context.Context, request contractsv1.GetLocalResourceRequest[*ctfv1.Repository], _ map[string]string) error {
	repo, err := p.createRepository(request.Repository)
	if err != nil {
		return fmt.Errorf("error creating repository: %w", err)
	}
	b, _, err := repo.GetLocalResource(ctx, request.Name, request.Version, request.Identity)

	return writeBlobToLocation(request.TargetLocation, b)
}

var (
	_ contractsv1.ReadWriteOCMRepositoryPluginContract[*ctfv1.Repository] = (*Plugin)(nil)
)

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

func writeBlobToLocation(location types.Location, b blob.ReadOnlyBlob) error {
	switch location.LocationType {
	case types.LocationTypeLocalFile:
		f, err := os.OpenFile(location.Value, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return fmt.Errorf("error opening file %q: %w", location.Value, err)
		}
		defer f.Close()
		if err := blob.Copy(f, b); err != nil {
			return fmt.Errorf("error copying blob to file %q: %w", location.Value, err)
		}
	case types.LocationTypeUnixNamedPipe:
		f, err := os.OpenFile(location.Value, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModeNamedPipe)
		if err != nil {
			return fmt.Errorf("error opening named pipe %q: %w", location.Value, err)
		}
		defer f.Close()
		if err := blob.Copy(f, b); err != nil {
			return fmt.Errorf("error copying blob to named pipe %q: %w", location.Value, err)
		}
	default:
		return fmt.Errorf("unsupported target location type %q", location.LocationType)
	}
	return nil
}

func readBlobFromLocation(location types.Location) (blob.ReadOnlyBlob, error) {
	var b blob.ReadOnlyBlob
	var err error
	switch location.LocationType {
	case types.LocationTypeLocalFile, types.LocationTypeUnixNamedPipe:
		if b, err = filesystem.GetBlobFromOSPath(location.Value); err != nil {
			return nil, fmt.Errorf("error getting blob from OS path: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported resource location type %q", location.LocationType)
	}
	return b, nil
}
