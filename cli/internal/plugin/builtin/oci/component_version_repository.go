package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/plugin/builtin/location"
)

type ComponentVersionRepositoryPlugin struct {
	contracts.EmptyBasePlugin
	manifests cache.OCIDescriptorCache
	layers    cache.OCIDescriptorCache
	repoCache *repoCache
}

// TODO Repeated calls with separate credentials will always use the first credentials set.
//
//	we need to be able to dynamically inject credentials to an existing repository instance.
func (p *ComponentVersionRepositoryPlugin) getRepository(spec *ociv1.Repository, creds map[string]string) (Repository, error) {
	key := spec.BaseUrl
	if repo, ok := p.repoCache.Get(key); ok {
		return repo, nil
	}
	repo, err := createRepository(spec, creds, p.manifests, p.layers)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	p.repoCache.Set(key, repo)
	return repo, nil
}

func (p *ComponentVersionRepositoryPlugin) GetIdentity(_ context.Context, typ *repov1.GetIdentityRequest[*ociv1.Repository]) (*repov1.GetIdentityResponse, error) {
	identity, err := runtime.ParseURLToIdentity(typ.Typ.BaseUrl)
	if err != nil {
		return nil, fmt.Errorf("error parsing URL to identity: %w", err)
	}
	identity.SetType(runtime.NewVersionedType(ociv1.Type, ociv1.Version))
	return &repov1.GetIdentityResponse{Identity: identity}, nil
}

func (p *ComponentVersionRepositoryPlugin) GetComponentVersion(ctx context.Context, request repov1.GetComponentVersionRequest[*ociv1.Repository], credentials map[string]string) (*descriptor.Descriptor, error) {
	repo, err := p.getRepository(request.Repository, credentials)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo.GetComponentVersion(ctx, request.Name, request.Version)
}

func (p *ComponentVersionRepositoryPlugin) ListComponentVersions(ctx context.Context, request repov1.ListComponentVersionsRequest[*ociv1.Repository], credentials map[string]string) ([]string, error) {
	repo, err := p.getRepository(request.Repository, credentials)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo.ListComponentVersions(ctx, request.Name)
}

func (p *ComponentVersionRepositoryPlugin) AddComponentVersion(ctx context.Context, request repov1.PostComponentVersionRequest[*ociv1.Repository], credentials map[string]string) error {
	repo, err := p.getRepository(request.Repository, credentials)
	if err != nil {
		return fmt.Errorf("error creating repository: %w", err)
	}
	desc, err := descriptor.ConvertFromV2(request.Descriptor)
	if err != nil {
		return fmt.Errorf("error converting descriptor: %w", err)
	}
	return repo.AddComponentVersion(ctx, desc)
}

func (p *ComponentVersionRepositoryPlugin) AddLocalResource(ctx context.Context, request repov1.PostLocalResourceRequest[*ociv1.Repository], credentials map[string]string) (*descriptor.Resource, error) {
	repo, err := p.getRepository(request.Repository, credentials)
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

func (p *ComponentVersionRepositoryPlugin) GetLocalResource(ctx context.Context, request repov1.GetLocalResourceRequest[*ociv1.Repository], credentials map[string]string) (repov1.GetLocalResourceResponse, error) {
	repo, err := p.getRepository(request.Repository, credentials)
	if err != nil {
		return repov1.GetLocalResourceResponse{}, fmt.Errorf("error creating repository: %w", err)
	}
	b, res, err := repo.GetLocalResource(ctx, request.Name, request.Version, request.Identity)
	if err != nil {
		return repov1.GetLocalResourceResponse{}, fmt.Errorf("error getting local resource: %w", err)
	}

	path := filepath.Join(os.TempDir(), fmt.Sprintf("ocm-local-resource-%d", res.ToIdentity().CanonicalHashV1()))
	tmp, err := os.Create(path)
	if err != nil {
		return repov1.GetLocalResourceResponse{}, fmt.Errorf("error creating buffer file: %w", err)
	}
	_ = tmp.Close() // Ensure the file is closed after creation

	loc := types.Location{
		LocationType: types.LocationTypeLocalFile,
		Value:        path,
	}

	if err := location.Write(loc, b); err != nil {
		return repov1.GetLocalResourceResponse{}, fmt.Errorf("error writing blob to location: %w", err)
	}

	return repov1.GetLocalResourceResponse{Location: loc}, nil
}
