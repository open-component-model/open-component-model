package plugin

import (
	"context"
	"fmt"
	"os"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/cache/inmemory"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
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
		&ociv1.Repository{},
	)
}

type Plugin struct {
	contracts.EmptyBasePlugin
	scheme *runtime.Scheme
	memory cache.OCIDescriptorCache
}

func (p *Plugin) GetIdentity(_ context.Context, typ contractsv1.GetIdentityRequest[*ociv1.Repository]) (runtime.Identity, error) {
	identity, err := runtime.ParseURLToIdentity(typ.Typ.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL to identity: %w", err)
	}
	identity.SetType(runtime.NewVersionedType(ociv1.Type, ociv1.Version))
	return identity, nil
}

func (p *Plugin) GetComponentVersion(ctx context.Context, request contractsv1.GetComponentVersionRequest[*ociv1.Repository], credentials map[string]string) (*descriptor.Descriptor, error) {
	repo, err := p.createRepository(request.Repository, credentials)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo.GetComponentVersion(ctx, request.Name, request.Version)
}

func (p *Plugin) AddComponentVersion(ctx context.Context, request contractsv1.PostComponentVersionRequest[*ociv1.Repository], credentials map[string]string) error {
	repo, err := p.createRepository(request.Repository, credentials)
	if err != nil {
		return fmt.Errorf("error creating repository: %w", err)
	}
	desc, err := descriptor.ConvertFromV2(request.Descriptor)
	if err != nil {
		return fmt.Errorf("error converting descriptor: %w", err)
	}
	return repo.AddComponentVersion(ctx, desc)
}

func (p *Plugin) AddLocalResource(ctx context.Context, request contractsv1.PostLocalResourceRequest[*ociv1.Repository], credentials map[string]string) (*descriptor.Resource, error) {
	repo, err := p.createRepository(request.Repository, credentials)
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

func (p *Plugin) GetLocalResource(ctx context.Context, request contractsv1.GetLocalResourceRequest[*ociv1.Repository], credentials map[string]string) error {
	repo, err := p.createRepository(request.Repository, credentials)
	if err != nil {
		return fmt.Errorf("error creating repository: %w", err)
	}
	b, _, err := repo.GetLocalResource(ctx, request.Name, request.Version, request.Identity)

	return writeBlobToLocation(request.TargetLocation, b)
}

var (
	_ contractsv1.ReadWriteOCMRepositoryPluginContract[*ociv1.Repository] = (*Plugin)(nil)
)

// TODO add identity mapping function from OCI package here as soon as we have the conversion function
func (p *Plugin) createRepository(spec *ociv1.Repository, credentials map[string]string) (oci.ComponentVersionRepository, error) {
	url, err := runtime.ParseURLAndAllowNoScheme(spec.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("invalid URL %q: %w", spec.BaseUrl, err)
	}
	urlString := url.Host + url.Path

	urlResolver := urlresolver.New(urlString)
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
		oci.WithOCIDescriptorCache(p.memory),
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
