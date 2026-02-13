package provider

import (
	"context"
	"errors"
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
// - An authorization cache for auth tokens
// - A shared HTTP client with retry capabilities
type CachingComponentVersionRepositoryProvider struct {
	// The creator is the creator of new Component Versions.
	// See AnnotationOCMCreator for details
	creator string

	scheme *runtime.Scheme

	// storeCache is a thread-safe cache implementation for caching instances
	// of the ctf store with the oci repository path as key.
	// The ctf is a file-based implementation of an oras oci store. Currently,
	// it relies on locks on the data structure level (instead of on the file level).
	// The cache avoids creating multiple stores operating on the same files,
	// which is required to avoid race conditions.
	storeCache *storeCache

	// The purpose of the cache is to be able to centrally update the credentials
	// also for repositories (including already existing repositories) provided
	// by this repository provider.
	credentialCache *credentialCache

	// authorizationCache caches the auth-scheme and auth-token for the
	// "Authorization" header in accessing the remote registry.
	// It is shared by all repositories provided by this provider.
	authorizationCache auth.Cache

	// httpClient is the shared HTTP client used by all repositories provided.
	httpClient *http.Client

	// tempDir is the shared default temporary filesystem directory for any
	// temporary data created by the repositories provided by the provider
	// (such as the extracted directory representation of a tar
	// or tar.gz ctf archive).
	tempDir string
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

	if options.Scheme == nil {
		options.Scheme = repoSpec.Scheme
	}

	provider := &CachingComponentVersionRepositoryProvider{
		creator:            options.UserAgent,
		scheme:             options.Scheme,
		storeCache:         &storeCache{store: make(map[string]*ocictf.Store)},
		credentialCache:    &credentialCache{},
		authorizationCache: auth.NewCache(),
		httpClient:         retry.DefaultClient,
		tempDir:            options.TempDir,
	}

	return provider
}

func (b *CachingComponentVersionRepositoryProvider) GetComponentVersionRepositoryScheme() *runtime.Scheme {
	return b.scheme
}

// GetJSONSchemaForRepositorySpecification provides the JSON schema for OCI and CTF repository specifications.
func (b *CachingComponentVersionRepositoryProvider) GetJSONSchemaForRepositorySpecification(typ runtime.Type) ([]byte, error) {
	obj, err := b.scheme.NewObject(typ)
	if err != nil {
		return nil, err
	}
	var schema []byte
	switch obj := obj.(type) {
	case *ocirepospecv1.Repository:
		schema = obj.JSONSchema()
	case *ctfrepospecv1.Repository:
		schema = obj.JSONSchema()
	}

	return schema, nil
}

// GetComponentVersionRepositoryCredentialConsumerIdentity implements the repository.ComponentVersionRepositoryProvider interface.
// It retrieves the consumer identity for a given repository specification.
func (b *CachingComponentVersionRepositoryProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(ctx context.Context, repositorySpecification runtime.Typed) (runtime.Identity, error) {
	obj, err := getConvertedTypedSpec(b.scheme, repositorySpecification)
	if err != nil {
		return nil, err
	}
	switch obj := obj.(type) {
	case *ocirepospecv1.Repository:
		return v1.IdentityFromOCIRepository(obj)
	case *ctfrepospecv1.Repository:
		return nil, errors.New("cannot resolve consumer identity for ctf: credentials not supported")
	default:
		return nil, fmt.Errorf("unsupported repository specification type for identity generation %T", obj)
	}
}

// getCacheIdentity is a helper function that extracts the consumer identity
// from a repository specification. It supports both OCI and CTF repository types.
func getCacheIdentity(_ context.Context, scheme *runtime.Scheme, repositorySpecification runtime.Typed) (runtime.Identity, error) {
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

	opts := []oci.RepositoryOption{
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
		// TODO(fabianburth): loadOrStore checks whether the cache already contains a store for
		//  the given path. If it does, it returns the cached store.
		//  If not, it calls loadFunc to create a new store, stores it in the cache,
		//  and then returns the newly created store.
		//  Without this cache, we would create multiple stores for the same path
		//  which would race on file access (https://github.com/open-component-model/ocm-project/issues/694).
		store, err := b.storeCache.loadOrStore(ctx, obj.FilePath, loadFunc)
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
	_, _ = scheme.DefaultType(repositorySpecification)
	obj, err := scheme.NewObject(repositorySpecification.GetType())
	if err != nil {
		return nil, err
	}
	if err := scheme.Convert(repositorySpecification, obj); err != nil {
		return nil, err
	}
	return obj, nil
}
