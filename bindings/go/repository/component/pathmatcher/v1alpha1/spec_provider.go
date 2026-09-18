package v1alpha1

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gobwas/glob"
	slogcontext "github.com/veqryn/slog-context"

	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

// compiledResolver holds a resolver together with its pre-compiled glob pattern
// and optional version constraint, so they are validated once at construction
// time instead of on every call.
type compiledResolver struct {
	resolver             *resolverspec.Resolver
	componentNamePattern glob.Glob
	hasConstraint        bool // true when a non-empty version constraint is set
}

// SpecProvider implements a ComponentVersionRepositorySpecProvider with
// a resolver mechanism. It uses path patterns leveraging the github.com/gobwas/glob
// library to match component names to determine which OCM repository
// specification to use for resolving component versions.
type SpecProvider struct {
	// A list of compiled resolvers to use for matching components to repositories.
	// This list is immutable after creation.
	resolvers []compiledResolver
	// registry defines the versioning schemes used to evaluate version
	// constraints. Defaults to loose semver.
	registry *versioning.Registry
}

// SpecProviderOption configures a [SpecProvider].
type SpecProviderOption func(*SpecProvider)

// WithVersioningRegistry sets the versioning schemes used to evaluate resolver
// version constraints. When unset, the loose-semver default is used.
func WithVersioningRegistry(registry *versioning.Registry) SpecProviderOption {
	return func(p *SpecProvider) {
		p.registry = registry
	}
}

// NewSpecProvider creates a new SpecProvider with a list of resolvers.
// The resolvers are used to match component names to repository specifications.
// A resolver with an empty component name pattern matches any component name.
// It returns an error if any resolver has an invalid glob pattern or version constraint.
func NewSpecProvider(_ context.Context, resolvers []*resolverspec.Resolver, opts ...SpecProviderOption) (*SpecProvider, error) {
	provider := &SpecProvider{registry: versioning.Default()}
	for _, opt := range opts {
		opt(provider)
	}

	compiled := make([]compiledResolver, 0, len(resolvers))
	for i, r := range resolvers {
		pattern := strings.TrimSpace(r.ComponentNamePattern)
		if pattern == "" {
			pattern = "*"
		}
		g, err := glob.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile glob pattern %q in resolver index %d: %w", pattern, i, err)
		}

		if r.VersionConstraint != "" {
			// Validate the constraint once at construction against the configured
			// schemes so an unusable constraint fails load rather than silently
			// matching nothing later.
			if err := provider.registry.ValidateConstraint(r.VersionConstraint); err != nil {
				return nil, fmt.Errorf("invalid version constraint %q in resolver index %d: %w", r.VersionConstraint, i, err)
			}
		}

		compiled = append(compiled, compiledResolver{
			resolver:             r,
			componentNamePattern: g,
			hasConstraint:        r.VersionConstraint != "",
		})
	}
	provider.resolvers = compiled
	return provider, nil
}

// GetRepositorySpec returns the repository specification for the given component identity.
// It matches the component name against the configured resolvers in list order and
// returns the repository specification of the first match. Resolvers after the first
// match are not consulted, so there is no fallback to another repository if the
// component version does not exist in the matched repository.
// If no matching resolver is found, an error is returned.
// componentIdentity must contain the key [descruntime.IdentityAttributeName] with the
// component name (e.g. "ocm.software/core/test") and may optionally contain
// [descruntime.IdentityAttributeVersion] with a semver version string.
func (r *SpecProvider) GetRepositorySpec(ctx context.Context, componentIdentity runtime.Identity) (runtime.Typed, error) {
	logger := slogcontext.FromCtx(ctx).With(slog.String("realm", "repository"))

	componentName, ok := componentIdentity[descruntime.IdentityAttributeName]
	if !ok {
		return nil, fmt.Errorf("failed to extract component name from identity %s", componentIdentity)
	}
	logger.Log(ctx, slog.LevelDebug, "resolving repository spec for component",
		slog.String("component", componentName),
		slog.Int("resolvers", len(r.resolvers)),
	)

	version := componentIdentity[descruntime.IdentityAttributeVersion]

	for index, cr := range r.resolvers {
		logger.Log(ctx, slog.LevelDebug, "checking resolver",
			slog.Int("index", index),
			slog.String("pattern", cr.resolver.ComponentNamePattern),
			slog.String("versionConstraint", cr.resolver.VersionConstraint),
		)
		if !cr.componentNamePattern.Match(componentName) {
			continue
		}

		if cr.hasConstraint {
			if version == "" {
				logger.Log(ctx, slog.LevelDebug, "skipping resolver with version constraint because no version was provided",
					slog.Int("index", index),
					slog.String("versionConstraint", cr.resolver.VersionConstraint),
				)
				continue
			}

			satisfied, err := r.registry.Satisfies(version, cr.resolver.VersionConstraint)
			if err != nil {
				logger.Log(ctx, slog.LevelDebug, "skipping resolver because version constraint could not be evaluated",
					slog.Int("index", index),
					slog.String("version", version),
					slog.String("error", err.Error()),
				)
				continue
			}
			if !satisfied {
				logger.Log(ctx, slog.LevelDebug, "version does not satisfy constraint",
					slog.Int("index", index),
					slog.String("version", version),
					slog.String("versionConstraint", cr.resolver.VersionConstraint),
				)
				continue
			}
		}

		logger.Log(ctx, slog.LevelDebug, "matched resolver",
			slog.String("Repository", cr.resolver.Repository.Name),
			slog.String("pattern", cr.resolver.ComponentNamePattern),
			slog.String("versionConstraint", cr.resolver.VersionConstraint),
		)
		return cr.resolver.Repository, nil
	}

	logger.
		Log(ctx, slog.LevelDebug, "no matching resolver found for component",
			slog.String("component", componentName),
		)
	return nil, repository.ErrNotFound
}
