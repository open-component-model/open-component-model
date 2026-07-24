package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"oras.land/oras-go/v2/registry/remote/auth"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/cache"
	"ocm.software/open-component-model/bindings/go/oci/credentials"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
	v2 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/identity/v1"
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
	// creator identifies the tool or library creating new Component Versions.
	// It is used in two distinct ways:
	//   1. OCM annotation: written as AnnotationOCMCreator on every component
	//      version added via AddComponentVersion (via oci.WithCreator).
	//   2. Auth-layer User-Agent: injected as the HTTP User-Agent header on
	//      authenticated OCI requests via auth.Client.Header.
	// Note: the transport-level User-Agent is set independently on the
	// httpClient via ocmhttp.WithUserAgent, so both layers carry the same
	// value but serve different purposes.
	creator string

	scheme *runtime.Scheme

	// storeCache is a thread-safe cache implementation for caching instances
	// of the ctf store with the oci repository path as key.
	// The ctf is a file-based implementation of an oras oci store. Currently,
	// it relies on locks on the data structure level (instead of on the file level).
	// The cache avoids creating multiple stores operating on the same files,
	// which is required to avoid race conditions.
	storeCache *storeCache

	// httpClient is the shared HTTP client used by all repositories provided.
	httpClient *http.Client

	// tempDir is the shared default temporary filesystem directory for any
	// temporary data created by the repositories provided by the provider
	// (such as the extracted directory representation of a tar
	// or tar.gz ctf archive).
	tempDir string

	// blobCacheOpts, when non-nil, enables a shared content-addressable blob
	// cache. All credential scopes share one BlobCache because blobs are
	// immutable and identified by digest — a digest unambiguously identifies
	// content regardless of who fetched it. Only tag→digest resolution is
	// access-controlled; once you hold a digest you are authorised.
	blobCacheOpts *cache.Options

	// referenceCacheOpts, when non-nil, enables per-scope reference caches.
	// Tag resolution IS access-controlled (a private registry won't return a
	// descriptor for a tag you can't read), so each credential scope gets its
	// own ReferenceCache to prevent one scope from reading tag mappings
	// resolved under a different credential set.
	referenceCacheOpts *cache.Options

	// sharedBlobCache is the single process-wide BlobCache shared across all
	// credential scopes. Initialised lazily on first use via sharedBlobOnce.
	sharedBlobCache *cache.BlobCache
	sharedBlobOnce  sync.Once

	// referenceCaches stores one *cache.ReferenceCache per credential scope key.
	referenceCaches sync.Map // string → *cache.ReferenceCache
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

	return &CachingComponentVersionRepositoryProvider{
		creator:    options.UserAgent,
		scheme:     options.Scheme,
		storeCache: &storeCache{store: make(map[string]*ocictf.Store)},
		httpClient: ocmhttp.New(
			ocmhttp.WithConfig(options.HTTPConfig),
			ocmhttp.WithUserAgent(options.UserAgent),
		),
		tempDir:            options.TempDir,
		blobCacheOpts:      options.BlobCacheOptions,
		referenceCacheOpts: options.ReferenceCacheOptions,
	}
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

// GetComponentVersionRepository implements the repository.ComponentVersionRepositoryProvider interface.
// It retrieves a component version repository with caching support for the given specification and credentials.
func (b *CachingComponentVersionRepositoryProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, creds runtime.Typed) (repository.ComponentVersionRepository, error) {
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
		identity, err := v1.OCIRegistryIdentityFromOCIRepository(obj)
		if err != nil {
			return nil, err
		}

		var ociCredentials *v2.OCICredentials
		if creds != nil {
			ociCredentials, err = v2.ConvertToOCICredentials(creds)
			if err != nil {
				return nil, fmt.Errorf("error converting credentials: %w", err)
			}
		}

		var resolverOpts []urlresolver.Option
		if b.blobCacheOpts != nil {
			if bc := b.getOrCreateBlobCache(); bc != nil {
				resolverOpts = append(resolverOpts, urlresolver.WithBlobCache(bc))
			}
		}
		if b.referenceCacheOpts != nil {
			if rc := b.getOrCreateReferenceCache(identity, ociCredentials); rc != nil {
				resolverOpts = append(resolverOpts, urlresolver.WithReferenceCache(rc))
			}
		}

		resolver, err := ocirepository.NewResolver(ctx, &auth.Client{
			Client:     b.httpClient,
			Cache:      auth.NewCache(),
			Credential: credentials.CredentialFunc(identity, ociCredentials),
			Header: map[string][]string{
				"User-Agent": {b.creator},
			},
		}, obj, resolverOpts...)
		if err != nil {
			return nil, fmt.Errorf("error creating oci repository resolver: %w", err)
		}
		opts = append(opts, oci.WithResolver(resolver))

		return oci.NewRepository(opts...)
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

// getOrCreateBlobCache returns the process-wide shared BlobCache, initialising
// it on first use. All credential scopes share one BlobCache because blobs are
// content-addressed by digest — a digest uniquely and immutably identifies
// content, so sharing across scopes cannot serve unexpected data. Deduplication
// is therefore safe and avoids redundant disk storage when the same blob is
// fetched by different callers.
func (b *CachingComponentVersionRepositoryProvider) getOrCreateBlobCache() *cache.BlobCache {
	b.sharedBlobOnce.Do(func() {
		opts := *b.blobCacheOpts
		if opts.Dir == "" {
			base := b.tempDir
			if base == "" {
				base = os.TempDir()
			}
			opts.Dir = filepath.Join(base, "ocm-oci-cas")
		}
		c, err := cache.NewBlobCache(opts)
		if err != nil {
			slog.Warn("provider: failed to initialise shared blob cache, continuing without caching",
				slog.String("err", err.Error()))
			return
		}
		b.sharedBlobCache = c
	})
	return b.sharedBlobCache
}

// getOrCreateReferenceCache returns the ReferenceCache for the given credential
// scope, creating and persisting it on first use.
func (b *CachingComponentVersionRepositoryProvider) getOrCreateReferenceCache(
	identity *v1.OCIRegistryIdentity,
	creds *v2.OCICredentials,
) *cache.ReferenceCache {
	scope := cache.ScopeKey(identity, creds)
	if v, ok := b.referenceCaches.Load(scope); ok {
		return v.(*cache.ReferenceCache)
	}

	opts := *b.referenceCacheOpts
	if opts.Dir == "" {
		base := b.tempDir
		if base == "" {
			base = os.TempDir()
		}
		opts.Dir = filepath.Join(base, "ocm-oci-refcache", scope)
	} else {
		opts.Dir = filepath.Join(opts.Dir, scope)
	}

	c, err := cache.NewReferenceCache(opts)
	if err != nil {
		slog.Warn("provider: failed to initialise reference cache for scope, continuing without caching",
			slog.String("scope", scope),
			slog.String("err", err.Error()))
		return nil
	}

	actual, _ := b.referenceCaches.LoadOrStore(scope, c)
	return actual.(*cache.ReferenceCache)
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
