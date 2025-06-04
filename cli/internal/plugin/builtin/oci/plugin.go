package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

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
		&ociv1.Repository{},
	)
}

type Plugin struct {
	contracts.EmptyBasePlugin
	scheme *runtime.Scheme
	memory cache.OCIDescriptorCache
}

func (p *Plugin) GetIdentity(_ context.Context, typ *contractsv1.GetIdentityRequest[*ociv1.Repository]) (*contractsv1.GetIdentityResponse, error) {
	identity, err := runtime.ParseURLToIdentity(typ.Typ.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL to identity: %w", err)
	}
	identity.SetType(runtime.NewVersionedType(ociv1.Type, ociv1.Version))
	return &contractsv1.GetIdentityResponse{Identity: identity}, nil
}

func (p *Plugin) GetComponentVersion(ctx context.Context, request contractsv1.GetComponentVersionRequest[*ociv1.Repository], credentials map[string]string) (*descriptor.Descriptor, error) {
	repo, err := p.createRepository(request.Repository, credentials)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo.GetComponentVersion(ctx, request.Name, request.Version)
}

func (p *Plugin) ListComponentVersions(ctx context.Context, request contractsv1.ListComponentVersionsRequest[*ociv1.Repository], credentials map[string]string) ([]string, error) {
	repo, err := p.createRepository(request.Repository, credentials)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo.ListComponentVersions(ctx, request.Name)
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

func (p *Plugin) GetLocalResource(ctx context.Context, request contractsv1.GetLocalResourceRequest[*ociv1.Repository], credentials map[string]string) (contractsv1.GetLocalResourceResponse, error) {
	repo, err := p.createRepository(request.Repository, credentials)
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

var _ contractsv1.ReadWriteOCMRepositoryPluginContract[*ociv1.Repository] = (*Plugin)(nil)

// TODO(jakobmoellerdev): add identity mapping function from OCI package here as soon as we have the conversion function
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
