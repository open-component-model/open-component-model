package plugin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	resourcev1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ResourceRepositoryPlugin struct {
	contracts.EmptyBasePlugin
	scheme    *runtime.Scheme
	memory    cache.OCIDescriptorCache
	repoCache *repoCache
}

// TODO Repeated calls with separate credentials will always use the first credentials set.
//
//	we need to be able to dynamically inject credentials to an existing repository instance.
func (p *ResourceRepositoryPlugin) getRepository(spec *ociv1.Repository, creds map[string]string) (Repository, error) {
	key := spec.BaseUrl
	if repo, ok := p.repoCache.Get(key); ok {
		return repo, nil
	}
	repo, err := createRepository(spec, creds, p.scheme, p.memory)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	p.repoCache.Set(key, repo)
	return repo, nil
}

func (p *ResourceRepositoryPlugin) GetIdentity(_ context.Context, req *resourcev1.GetIdentityRequest[runtime.Typed]) (*resourcev1.GetIdentityResponse, error) {
	t := req.Typ.GetType()
	obj, err := p.scheme.NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	switch access := obj.(type) {
	case *v1.OCIImage:
		baseURL, err := ociImageAccessToBaseURL(access)
		if err != nil {
			return nil, fmt.Errorf("error creating oci image access: %w", err)
		}
		identity, err := runtime.ParseURLToIdentity(baseURL)
		if err != nil {
			return nil, fmt.Errorf("error parsing URL to identity: %w", err)
		}
		identity.SetType(runtime.NewVersionedType(ociv1.Type, ociv1.Version))
		return &resourcev1.GetIdentityResponse{Identity: identity}, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for GetIdentity", t)
	}
}

func (p *ResourceRepositoryPlugin) GetGlobalResource(ctx context.Context, request *resourcev1.GetResourceRequest, credentials map[string]string) (*resourcev1.GetGlobalResourceResponse, error) {
	t := request.Access.GetType()
	obj, err := p.scheme.NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	switch access := obj.(type) {
	case *v1.OCIImage:
		baseURL, err := ociImageAccessToBaseURL(access)
		if err != nil {
			return nil, fmt.Errorf("error creating oci image access: %w", err)
		}

		repo, err := p.getRepository(&ociv1.Repository{
			BaseUrl: baseURL,
		}, credentials)
		if err != nil {
			return nil, fmt.Errorf("error creating repository: %w", err)
		}

		res := descriptor.ConvertFromV2Resources([]v2.Resource{*request.Resource})[0]

		blob, err := repo.DownloadResource(ctx, &res)
		if err != nil {
			return nil, fmt.Errorf("error downloading resource: %w", err)
		}

		path := filepath.Join(os.TempDir(), fmt.Sprintf("ocm-global-resource-%d", res.ToIdentity().CanonicalHashV1()))

		if err := filesystem.CopyBlobToOSPath(blob, path); err != nil {
			return nil, fmt.Errorf("error copying blob to OS path: %w", err)
		}

		return &resourcev1.GetGlobalResourceResponse{
			ResourceLocation: types.Location{
				LocationType: types.LocationTypeLocalFile,
				Value:        path,
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for GetGlobalResource", t)
	}
}

func (p *ResourceRepositoryPlugin) AddGlobalResource(ctx context.Context, request *resourcev1.PostResourceRequest, credentials map[string]string) (*resourcev1.AddGlobalResourceResponse, error) {
	t := request.Resource.Access.GetType()
	obj, err := p.scheme.NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	switch access := obj.(type) {
	case *v1.OCIImage:
		baseURL, err := ociImageAccessToBaseURL(access)
		if err != nil {
			return nil, fmt.Errorf("error creating oci image access: %w", err)
		}

		repo, err := p.getRepository(&ociv1.Repository{
			BaseUrl: baseURL,
		}, credentials)
		if err != nil {
			return nil, fmt.Errorf("error creating repository: %w", err)
		}

		res := descriptor.ConvertFromV2Resources([]v2.Resource{*request.Resource})[0]

		if request.ResourceLocation.LocationType != types.LocationTypeLocalFile {
			return nil, fmt.Errorf("unsupported resource location type %s for AddGlobalResource", request.ResourceLocation.LocationType)
		}

		blob, err := filesystem.GetBlobFromOSPath(request.ResourceLocation.Value)
		if err != nil {
			return nil, fmt.Errorf("error getting blob from OS path: %w", err)
		}

		resAfterUpload, err := repo.UploadResource(ctx, request.Resource.Access, &res, blob)
		if err != nil {
			return nil, fmt.Errorf("error downloading resource: %w", err)
		}

		runtimeResAfterUpload, err := descriptor.ConvertToV2Resources(p.scheme, []descriptor.Resource{*resAfterUpload})
		if err != nil {
			return nil, fmt.Errorf("error converting resource after upload: %w", err)
		}

		return &resourcev1.AddGlobalResourceResponse{
			Resource: &runtimeResAfterUpload[0],
		}, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for GetGlobalResource", t)
	}
}

func ociImageAccessToBaseURL(access *v1.OCIImage) (string, error) {
	ref, err := registry.ParseReference(access.ImageReference)
	if err != nil {
		return "", fmt.Errorf("error parsing image reference %q: %w", access.ImageReference, err)
	}
	baseURL := ref.Host()
	if ref.Repository != "" {
		baseURL += "/" + ref.Repository
	}
	return baseURL, nil
}
