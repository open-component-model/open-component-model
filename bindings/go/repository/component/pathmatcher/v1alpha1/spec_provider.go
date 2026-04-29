package v1alpha1

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/Masterminds/semver/v3"
	"github.com/gobwas/glob"
	slogcontext "github.com/veqryn/slog-context"

	resolverspec "ocm.software/open-component-model/bindings/go/configuration/resolvers/v1alpha1/spec"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// compiledResolver holds a resolver together with its pre-compiled glob pattern
// and optional semver constraint, so they are validated once at construction time
// instead of on every call.
type compiledResolver struct {
	resolver             *resolverspec.Resolver
	componentNamePattern glob.Glob
	versionConstraint    *semver.Constraints // nil when no version constraint is set
}

// SpecProvider implements a ComponentVersionRepositorySpecProvider with
// a resolver mechanism. It uses path patterns leveraging the github.com/gobwas/glob
// library to match component names to determine which OCM repository
// specification to use for resolving component versions.
type SpecProvider struct {
	// A list of compiled resolvers to use for matching components to repositories.
	// This list is immutable after creation.
	resolvers []compiledResolver
}

// NewSpecProvider creates a new SpecProvider with a list of resolvers.
// The resolvers are used to match component names to repository specifications.
// It returns an error if any resolver has an invalid glob pattern or version constraint.
func NewSpecProvider(_ context.Context, resolvers []*resolverspec.Resolver) (*SpecProvider, error) {
	compiled := make([]compiledResolver, 0, len(resolvers))
	for i, r := range resolvers {
		g, err := glob.Compile(r.ComponentNamePattern)
		if err != nil {
			return nil, fmt.Errorf("failed to compile glob pattern %q in resolver index %d: %w", r.ComponentNamePattern, i, err)
		}

		var constraint *semver.Constraints
		if r.VersionConstraint != "" {
			c, err := semver.NewConstraint(r.VersionConstraint)
			if err != nil {
				return nil, fmt.Errorf("failed to parse version constraint %q in resolver index %d: %w", r.VersionConstraint, i, err)
			}
			constraint = c
		}

		compiled = append(compiled, compiledResolver{
			resolver:             r,
			componentNamePattern: g,
			versionConstraint:    constraint,
		})
	}
	return &SpecProvider{
		resolvers: compiled,
	}, nil
}

// GetRepositorySpec returns the repository specification for the given component identity.
// It matches the component name against the configured resolvers and returns
// the first matching repository specification.
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

		if cr.versionConstraint != nil {
			if version == "" {
				logger.Log(ctx, slog.LevelDebug, "skipping resolver with version constraint because no version was provided",
					slog.Int("index", index),
					slog.String("versionConstraint", cr.resolver.VersionConstraint),
				)
				continue
			}

			ver, err := semver.NewVersion(version)
			if err != nil {
				logger.Log(ctx, slog.LevelDebug, "skipping resolver because version is not valid semver",
					slog.Int("index", index),
					slog.String("version", version),
					slog.String("error", err.Error()),
				)
				continue
			}

			if !cr.versionConstraint.Check(ver) {
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
