package provider

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"

	ownershipv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/ownership/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/credentials"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/repository"
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

	// httpClient is the shared HTTP client used by all repositories provided.
	httpClient *http.Client

	// tempDir is the shared default temporary filesystem directory for any
	// temporary data created by the repositories provided by the provider
	// (such as the extracted directory representation of a tar
	// or tar.gz ctf archive).
	tempDir string

	// ownershipConfig is the resolved ownership referrer configuration.
	ownershipConfig *ownershipv1alpha1.Config
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
		creator:         options.UserAgent,
		scheme:          options.Scheme,
		storeCache:      &storeCache{store: make(map[string]*ocictf.Store)},
		httpClient:      retry.DefaultClient,
		tempDir:         options.TempDir,
		ownershipConfig: options.OwnershipConfig,
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

// GetComponentVersionRepository implements the repository.ComponentVersionRepositoryProvider interface.
// It retrieves a component version repository with caching support for the given specification and credentials.
func (b *CachingComponentVersionRepositoryProvider) GetComponentVersionRepository(ctx context.Context, repositorySpecification runtime.Typed, creds runtime.Typed) (repository.ComponentVersionRepository, error) {
	obj, err := getConvertedTypedSpec(b.scheme, repositorySpecification)
	if err != nil {
		return nil, err
	}

	// Ownership referrers are an OCI-only concept; CTF archives skip the lookup
	// and use the zero-value (Never) policy.
	var ownershipReferrerPolicy oci.OwnershipReferrerPolicy
	if _, ok := obj.(*ocirepospecv1.Repository); ok {
		ownershipReferrerPolicy, err = resolveOwnershipReferrerPolicy(b.scheme, b.ownershipConfig, obj)
		if err != nil {
			return nil, fmt.Errorf("resolving ownership referrer policy failed: %w", err)
		}
	}

	opts := []oci.RepositoryOption{
		oci.WithTempDir(b.tempDir),
		oci.WithCreator(b.creator),
		oci.WithOwnershipReferrerPolicy(ownershipReferrerPolicy),
	}

	switch obj := obj.(type) {
	case *ocirepospecv1.Repository:
		identity, err := v1.OCIRegistryIdentityFromOCIRepository(obj)
		if err != nil {
			return nil, err
		}

		ociCredentials, err := v2.ConvertToOCICredentials(creds)
		if err != nil {
			return nil, fmt.Errorf("error converting credentials: %w", err)
		}

		return ocirepository.NewFromOCIRepoV1(ctx, obj, &auth.Client{
			Client:     b.httpClient,
			Cache:      auth.NewCache(),
			Credential: credentials.CredentialFunc(identity, ociCredentials),
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

// resolveOwnershipReferrerPolicy returns the [oci.OwnershipReferrerPolicy]
// for the target repository, derived from the ownership configuration.
// Lookup order:
//
//  1. An entry whose identity equals the target's exactly wins.
//  2. Otherwise the first entry whose identity is a subset of the target's
//     wins (see [runtime.IdentitySubset]).
//  3. Otherwise the top-level cfg.Policy applies, defaulting to
//     [oci.OwnershipReferrerPolicyNever].
//
// Both the top-level policy and any matching entry policy are validated, so a
// malformed value is reported regardless of which one ends up applying.
func resolveOwnershipReferrerPolicy(scheme *runtime.Scheme, cfg *ownershipv1alpha1.Config, target runtime.Typed) (oci.OwnershipReferrerPolicy, error) {
	if cfg == nil {
		return oci.OwnershipReferrerPolicyNever, nil
	}

	// Validate the top-level policy up front so a malformed value is reported
	// even when a repository entry override ends up taking precedence.
	if _, err := ociOwnershipReferrerPolicy(cfg.Policy); err != nil {
		return oci.OwnershipReferrerPolicyNever, err
	}

	policy := cfg.Policy
	if len(cfg.Repositories) > 0 {
		match, err := matchOwnershipPolicy(scheme, target, cfg.Repositories)
		if err != nil {
			return oci.OwnershipReferrerPolicyNever, err
		}
		if match != nil {
			policy = match.Policy
		}
	}

	return ociOwnershipReferrerPolicy(policy)
}

// ociOwnershipReferrerPolicy maps an ownership configuration policy onto the
// oci binding's [oci.OwnershipReferrerPolicy]. An empty policy defaults to
// Never; an unrecognised value is rejected.
func ociOwnershipReferrerPolicy(policy ownershipv1alpha1.Policy) (oci.OwnershipReferrerPolicy, error) {
	switch policy {
	case "", ownershipv1alpha1.PolicyNever:
		return oci.OwnershipReferrerPolicyNever, nil
	case ownershipv1alpha1.PolicyAddIfSupported:
		return oci.OwnershipReferrerPolicyAddIfSupported, nil
	default:
		return oci.OwnershipReferrerPolicyNever, fmt.Errorf("unsupported ownership policy %q", policy)
	}
}

// matchOwnershipPolicy returns the entry that applies to target, or nil if
// none match. An exact identity match wins. Otherwise the first entry whose
// identity is a subset of the target's wins. Entries that are not OCI
// repositories (foreign or unregistered types) are skipped.
func matchOwnershipPolicy(scheme *runtime.Scheme, target runtime.Typed, repos []*ownershipv1alpha1.RepositoryPolicy) (*ownershipv1alpha1.RepositoryPolicy, error) {
	targetRepo, err := convertToOCIRepository(scheme, target)
	if err != nil {
		return nil, fmt.Errorf("invalid target repository spec: %w", err)
	}
	targetRepoIdentity, err := ociRepositoryIdentity(targetRepo)
	if err != nil {
		return nil, fmt.Errorf("invalid target repository spec: %w", err)
	}

	var match *ownershipv1alpha1.RepositoryPolicy
	for _, rp := range repos {
		if rp == nil || rp.Repository == nil {
			continue
		}
		// Resolve the entry through the scheme so registered aliases and short
		// forms map to the canonical type. Entries that are not OCI
		// repositories (foreign or unregistered types) cannot match an OCI
		// target and are skipped.
		entryRepo, err := convertToOCIRepository(scheme, rp.Repository)
		if err != nil {
			continue
		}
		identity, err := ociRepositoryIdentity(entryRepo)
		if err != nil {
			return nil, fmt.Errorf("invalid ownership repository policy entry: %w", err)
		}
		if identity.Equal(targetRepoIdentity) {
			return rp, nil
		}
		// Subset match: entry has fewer fields than target (e.g. baseUrl
		// alone matches a baseUrl+subPath target). Take the first one;
		// keep scanning so a later exact match can still win.
		if match == nil && identity.Match(targetRepoIdentity, runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			match = rp
		}
	}
	return match, nil
}

// convertToOCIRepository resolves a typed repository specification to a
// concrete [*ocirepospecv1.Repository] through the scheme, so registered
// aliases and short forms map to the canonical type. An already-converted
// spec is returned as-is. It returns an error when the type is unknown or is
// not an OCI repository.
func convertToOCIRepository(scheme *runtime.Scheme, spec runtime.Typed) (*ocirepospecv1.Repository, error) {
	if repo, ok := spec.(*ocirepospecv1.Repository); ok {
		return repo, nil
	}
	obj, err := getConvertedTypedSpec(scheme, spec)
	if err != nil {
		return nil, err
	}
	repo, ok := obj.(*ocirepospecv1.Repository)
	if !ok {
		return nil, fmt.Errorf("unsupported repository specification type %T", obj)
	}
	return repo, nil
}

// ociRepositoryIdentity returns the identity used to match a policy entry
// against an upload target. The full path — an embedded baseUrl path joined
// with any explicit subPath — is part of the identity.
func ociRepositoryIdentity(repo *ocirepospecv1.Repository) (runtime.Identity, error) {
	id, err := v1.IdentityFromOCIRepository(repo)
	if err != nil {
		return nil, fmt.Errorf("deriving identity from OCI repository spec failed: %w", err)
	}
	if repo.SubPath != "" {
		if existing := id[runtime.IdentityAttributePath]; existing != "" {
			id[runtime.IdentityAttributePath] = existing + "/" + repo.SubPath
		} else {
			id[runtime.IdentityAttributePath] = repo.SubPath
		}
	}
	return id, nil
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
