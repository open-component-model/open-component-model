package resolution

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
	"github.com/go-logr/logr"
	"github.com/hashicorp/golang-lru/v2/expirable"

	genericv1 "ocm.software/open-component-model/bindings/go/configuration/generic/v1/spec"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/setup"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/verification"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/pkg/configuration"
	ocirepository "ocm.software/open-component-model/bindings/go/oci/spec/repository"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	repoCacheSize = 100
	// A cached resolver pins the plugin manager and credential graph it was built with, so it must not live forever.
	repoCacheTTL = 30 * time.Minute
)

// NewResolver creates a new component version resolver.
func NewResolver(logger *logr.Logger) *Resolver {
	return &Resolver{
		logger:    logger,
		repoCache: expirable.NewLRU[string, *resolvedProvider](repoCacheSize, nil, repoCacheTTL),
	}
}

// Resolver fetches and verifies component versions. The repository resolvers it builds are cached per
// configuration and repository spec for a limited time.
type Resolver struct {
	logger    *logr.Logger
	repoCache *expirable.LRU[string, *resolvedProvider]
}

// Options contains everything the Resolver requires to locate a repository.
type Options struct {
	RepositorySpec runtime.Typed
	Configuration  *configuration.Configuration
	PluginManager  *manager.PluginManager
}

// Verification selects how a fetched component version is verified. Verifications and Digest are mutually
// exclusive; the zero value skips verification.
type Verification struct {
	// Verifications are the component signatures to verify.
	Verifications []verification.Verification
	// Digest is used to verify the integrity of a referenced component version.
	Digest *v2.Digest
}

// RepositoryResolver returns the resolver that picks the appropriate repository for each component based on:
// 1. Path matcher resolvers from OCM configuration (if configured)
// 2. The provided RepositorySpec as a fallback
func (r *Resolver) RepositoryResolver(ctx context.Context, opts *Options) (resolvers.ComponentVersionRepositoryResolver, error) {
	resolved, err := r.provider(ctx, opts)
	if err != nil {
		return nil, err
	}

	return resolved.resolver, nil
}

// GetComponentVersion fetches a component version and verifies it as requested.
// A component version that is not safely digestible is returned together with an ErrNotSafelyDigestible error.
func (r *Resolver) GetComponentVersion(ctx context.Context, opts *Options, verify Verification, component, version string) (*descriptor.Descriptor, error) {
	resolved, err := r.provider(ctx, opts)
	if err != nil {
		return nil, err
	}

	repo, err := resolved.resolver.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository for component %s:%s: %w", component, version, err)
	}

	start := time.Now()
	desc, err := getComponentVersion(ctx, resolveOptions{
		component:       component,
		version:         version,
		repository:      repo,
		verifications:   verify.Verifications,
		credentialGraph: resolved.credentialGraph,
		digest:          verify.Digest,
		signingRegistry: opts.PluginManager.SigningRegistry,
	})
	ResolutionDurationHistogram.
		WithLabelValues(component, version, verificationState(verify.Verifications, verify.Digest)).
		Observe(time.Since(start).Seconds())

	return desc, err
}

// resolvedProvider bundles the repository resolver with the credential graph that it was created with.
type resolvedProvider struct {
	resolver        resolvers.ComponentVersionRepositoryResolver
	credentialGraph credentials.Resolver
}

func (r *Resolver) provider(ctx context.Context, opts *Options) (*resolvedProvider, error) {
	if opts.PluginManager == nil {
		return nil, fmt.Errorf("plugin manager is required")
	}
	if opts.RepositorySpec == nil {
		return nil, fmt.Errorf("base repository spec is required")
	}

	cfg := opts.Configuration
	var configHash []byte
	if cfg != nil {
		configHash = cfg.Hash
	}
	cacheKey, err := buildRepoCacheKey(configHash, opts.RepositorySpec)
	if err != nil {
		return nil, fmt.Errorf("failed to build repository cache key: %w", err)
	}

	if resolved, ok := r.repoCache.Get(cacheKey); ok {
		return resolved, nil
	}

	resolved, err := r.createResolver(ctx, opts.RepositorySpec, cfg, opts.PluginManager)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}
	r.repoCache.Add(cacheKey, resolved)

	return resolved, nil
}

// createResolver creates a resolver based on the configuration.
// The resolver handles resolving the appropriate repository for each component.
func (r *Resolver) createResolver(ctx context.Context, spec runtime.Typed, cfg *configuration.Configuration, pm *manager.PluginManager) (*resolvedProvider, error) {
	opts := resolvers.Options{
		RepoProvider: pm.ComponentVersionRepositoryRegistry,
	}

	var genericCfg *genericv1.Config
	if cfg != nil {
		credGraph, err := setup.NewCredentialGraph(ctx, cfg.Config, setup.CredentialGraphOptions{
			PluginManager: pm,
			Logger:        r.logger,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create credential graph: %w", err)
		}
		r.logger.V(1).Info("resolved credential graph")
		opts.CredentialGraph = credGraph
		genericCfg = cfg.Config
	}

	resolver, err := resolvers.NewFromConfig(ctx, genericCfg, ocirepository.Scheme, opts, spec)
	if err != nil {
		return nil, err
	}

	return &resolvedProvider{resolver: resolver, credentialGraph: opts.CredentialGraph}, nil
}

// buildRepoCacheKey generates a cache key from the configuration hash and repository spec.
// It canonicalizes the repository spec using JCS (RFC 8785) before hashing to ensure consistent keys
// regardless of field ordering in the JSON representation.
func buildRepoCacheKey(configHash []byte, repoSpec runtime.Typed) (string, error) {
	repoJSON, err := json.Marshal(repoSpec)
	if err != nil {
		return "", fmt.Errorf("failed to marshal repository spec: %w", err)
	}

	canonicalJSON, err := jsoncanonicalizer.Transform(repoJSON)
	if err != nil {
		return "", fmt.Errorf("failed to canonicalize repository spec: %w", err)
	}

	hasher := fnv.New64a()
	// can safely ignore because fnv.Write never actually returns an error
	_, _ = hasher.Write(configHash)
	_, _ = hasher.Write(canonicalJSON)

	return fmt.Sprintf("%016x", hasher.Sum64()), nil
}
