package provider

import (
	"context"
	"fmt"
	"net/http"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	repoSpec "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfrepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// CachingComponentVersionRepositoryProvider is a caching implementation of the ComponentVersionRepositoryProvider interface.
// It uses multiple caches to store credentials, OCI descriptors, and authorization information for multiple repository specifications as well as a shared client.
type CachingComponentVersionRepositoryProvider struct {
	scheme *runtime.Scheme
	*credentialCache
	*ociCache
	authorizationCache auth.Cache
	httpClient         *http.Client
}

func NewComponentVersionRepositoryProvider() ComponentVersionRepositoryProvider {
	return &CachingComponentVersionRepositoryProvider{
		scheme:             repoSpec.Scheme,
		credentialCache:    &credentialCache{},
		ociCache:           &ociCache{scheme: repoSpec.Scheme},
		authorizationCache: auth.NewCache(),
		httpClient:         retry.DefaultClient,
	}
}

func (b *CachingComponentVersionRepositoryProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, b.scheme, repositorySpecification)
}

func GetComponentVersionRepositoryCredentialConsumerIdentity(_ context.Context, scheme *runtime.Scheme, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	if _, err := scheme.DefaultType(repositorySpecification); err != nil {
		return nil, fmt.Errorf("failed to ensure type for repository specification: %w", err)
	}
	obj, err := scheme.NewObject(repositorySpecification.GetType())
	if err != nil {
		return nil, err
	}
	if err := scheme.Convert(repositorySpecification, obj); err != nil {
		return nil, err
	}
	switch obj := obj.(type) {
	case *ocirepospecv1.Repository:
		return v1.IdentityFromOCIRepository(obj)
	case *ctfrepospecv1.Repository:
		return v1.IdentityFromCTFRepository(obj)
	default:
		return nil, fmt.Errorf("unsupported repository specification type for identity generation %T", obj)
	}
}

func (b *CachingComponentVersionRepositoryProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (oci.ComponentVersionRepository, error) {
	if _, err := b.scheme.DefaultType(repositorySpecification); err != nil {
		return nil, fmt.Errorf("failed to ensure type for repository specification: %w", err)
	}
	obj, err := b.scheme.NewObject(repositorySpecification.GetType())
	if err != nil {
		return nil, err
	}
	if err := b.scheme.Convert(repositorySpecification, obj); err != nil {
		return nil, err
	}

	manifests, layers, err := b.ociCache.get(ctx, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to getOCIDescriptors from repository cache: %w", err)
	}

	opts := []oci.RepositoryOption{
		oci.WithManifestCache(manifests),
		oci.WithLayerCache(layers),
	}

	switch obj := obj.(type) {
	case *ocirepospecv1.Repository:
		if err := b.credentialCache.add(obj, credentials); err != nil {
			return nil, fmt.Errorf("failed to add repository get to credentials: %w", err)
		}
		return repository.NewFromOCIRepoV1(ctx, obj, &auth.Client{
			Client:     b.httpClient,
			Cache:      b.authorizationCache,
			Credential: b.credentialCache.get,
		}, opts...)
	case *ctfrepospecv1.Repository:
		return repository.NewFromCTFRepoV1(ctx, obj, opts...)
	default:
		return nil, fmt.Errorf("unsupported repository specification type %T", obj)
	}
}
