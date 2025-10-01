package provider

import (
	"context"
	"fmt"
	"net/http"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/identity/v1"
	repoSpec "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	ctfrepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ocirepospecv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const DefaultCreator = "ocm.software/open-component-model/bindings/go/oci"

// CachingComponentVersionRepositoryProvider is a caching implementation of the repository.ComponentVersionRepositoryProvider interface.
// It provides efficient caching mechanisms for repository operations by maintaining:
// - A credential cache for authentication information
// - An OCI cache for manifests and layers
// - An authorization cache for auth tokens
// - A shared HTTP client with retry capabilities
type CachingComponentVersionRepositoryProvider struct {
	creator            string
	scheme             *runtime.Scheme
	storeCache         *storeCache
	credentialCache    *credentialCache
	ociCache           *ociCache
	authorizationCache auth.Cache
	httpClient         *http.Client
	tempDir            string
}

var _ repository.ComponentVersionRepositoryProvider = (*CachingComponentVersionRepositoryProvider)(nil)

// NewComponentVersionRepositoryProvider creates a new instance of CachingComponentVersionRepositoryProvider
// with initialized caches and default HTTP client configuration.
func NewComponentVersionRepositoryProvider(opts ...Option) *CachingComponentVersionRepositoryProvider {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	if options.UserAgent == "" {
		options.UserAgent = DefaultCreator
	}

	provider := &CachingComponentVersionRepositoryProvider{
		creator:            options.UserAgent,
		scheme:             repoSpec.Scheme,
		storeCache:         &storeCache{store: make(map[string]*ocictf.Store)},
		credentialCache:    &credentialCache{},
		ociCache:           &ociCache{scheme: repoSpec.Scheme},
		authorizationCache: auth.NewCache(),
		httpClient:         retry.DefaultClient,
		tempDir:            options.TempDir,
	}

	return provider
}

// GetComponentVersionRepositoryCredentialConsumerIdentity implements the repository.ComponentVersionRepositoryProvider interface.
// It retrieves the consumer identity for a given repository specification.
func (b *CachingComponentVersionRepositoryProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	return GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, b.scheme, repositorySpecification)
}

// GetComponentVersionRepositoryCredentialConsumerIdentity is a helper function that extracts the consumer identity
// from a repository specification. It supports both OCI and CTF repository types.
func GetComponentVersionRepositoryCredentialConsumerIdentity(_ context.Context, scheme *runtime.Scheme, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	obj, err := getConvertedTypedSpec(scheme, repositorySpecification)
	if err != nil {
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

// GetComponentVersionRepository implements the repository.ComponentVersionRepositoryProvider interface.
// It retrieves a component version repository with caching support for the given specification and credentials.
func (b *CachingComponentVersionRepositoryProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, credentials map[string]string) (repository.ComponentVersionRepository, error) {
	obj, err := getConvertedTypedSpec(b.scheme, repositorySpecification)
	if err != nil {
		return nil, err
	}

	manifests, layers, err := b.ociCache.get(ctx, obj)
	if err != nil {
		return nil, fmt.Errorf("failed to getOCIDescriptors from repository cache: %w", err)
	}

	opts := []oci.RepositoryOption{
		oci.WithManifestCache(manifests),
		oci.WithLayerCache(layers),
		oci.WithTempDir(b.tempDir),
		oci.WithCreator(b.creator),
	}

	switch obj := obj.(type) {
	case *ocirepospecv1.Repository:
		if err := b.credentialCache.add(obj, credentials); err != nil {
			return nil, fmt.Errorf("failed to add repository get to credentials: %w", err)
		}
		return ocirepository.NewFromOCIRepoV1(ctx, obj, &auth.Client{
			Client:     b.httpClient,
			Cache:      b.authorizationCache,
			Credential: b.credentialCache.get,
			Header: map[string][]string{
				"User-Agent": {b.creator},
			},
		}, opts...)
	case *ctfrepospecv1.Repository:
		loadFunc := func(path string) (*ocictf.Store, error) {
			return ocirepository.NewStoreFromCTFRepoV1(ctx, obj, opts...)
		}
		store, err := b.storeCache.loadOrStore(ctx, obj.Path, loadFunc)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve store from cache: %w", err)
		}
		repo, err := oci.NewRepository(append(opts, ocictf.WithCTF(store))...)
		if err != nil {
			return nil, fmt.Errorf("failed to create ctf repo from spec: %w", err)
		}
		return repo, nil
	default:
		return nil, fmt.Errorf("unsupported repository specification type %T", obj)
	}
}

// getConvertedTypedSpec is a helper function that converts any runtime.Typed specification
// to its corresponding object type in the scheme. It ensures that the type is set correctly
func getConvertedTypedSpec(scheme *runtime.Scheme, repositorySpecification runtime.Typed) (runtime.Typed, error) {
	repositorySpecification = repositorySpecification.DeepCopyTyped()
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
	return obj, nil
}
