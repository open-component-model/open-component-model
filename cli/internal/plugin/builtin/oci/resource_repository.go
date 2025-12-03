package oci

import (
	"context"
	"fmt"

	"oras.land/oras-go/v2/registry"

	"ocm.software/open-component-model/bindings/go/blob"
	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ResourceRepositoryPlugin struct {
	manifests, layers cache.OCIDescriptorCache
	filesystemConfig  *filesystemv1alpha1.Config
}

func (p *ResourceRepositoryPlugin) GetResourceRepositoryScheme() *runtime.Scheme {
	return ociaccess.Scheme
}

func (p *ResourceRepositoryPlugin) GetResourceDigestProcessorCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	t := resource.Access.GetType()
	obj, err := p.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := p.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
	}
	return p.getIdentity(obj)
}

func (p *ResourceRepositoryPlugin) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	t := resource.Access.GetType()
	obj, err := p.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := p.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
	}
	return p.getIdentity(obj)
}

func (p *ResourceRepositoryPlugin) ProcessResourceDigest(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (*descriptor.Resource, error) {
	t := resource.Access.GetType()
	obj, err := p.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := p.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
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

		resource = resource.DeepCopy()
		resource.Access = access

		resource, err := repo.ProcessResourceDigest(ctx, resource)
		if err != nil {
			return nil, fmt.Errorf("error downloading resource: %w", err)
		}

		return resource, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for downloading the resource", t)
	}
}

func (p *ResourceRepositoryPlugin) getIdentity(obj runtime.Typed) (runtime.Identity, error) {
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
		return identity, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for getting identity", obj.GetType())
	}
}

func (p *ResourceRepositoryPlugin) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials map[string]string) (blob.ReadOnlyBlob, error) {
	t := resource.Access.GetType()
	obj, err := p.GetResourceRepositoryScheme().NewObject(t)
	if err != nil {
		return nil, fmt.Errorf("error creating new object for type %s: %w", t, err)
	}
	if err := p.GetResourceRepositoryScheme().Convert(resource.Access, obj); err != nil {
		return nil, fmt.Errorf("error converting access to object of type %s: %w", t, err)
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

		b, err := repo.DownloadResource(ctx, resource)
		if err != nil {
			return nil, fmt.Errorf("error downloading resource: %w", err)
		}

		return b, nil
	default:
		return nil, fmt.Errorf("unsupported type %s for downloading the resource", t)
	}
}

func (p *ResourceRepositoryPlugin) getRepository(spec *ociv1.Repository, creds map[string]string) (Repository, error) {
	repo, err := createRepository(spec, creds, p.manifests, p.layers, p.filesystemConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating repository: %w", err)
	}
	return repo, nil
}

func ociImageAccessToBaseURL(access *v1.OCIImage) (string, error) {
	ref, err := registry.ParseReference(access.ImageReference)
	if err != nil {
		return "", fmt.Errorf("error parsing image reference %q: %w", access.ImageReference, err)
	}
	// host is the registry with sane defaulting
	baseURL := ref.Host()
	return baseURL, nil
}
