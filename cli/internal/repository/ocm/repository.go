package ocm

import (
	"context"
	"fmt"
	"sync"

	"github.com/Masterminds/semver/v3"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/reference/compref"
)

type ComponentRepository struct {
	ref         *compref.Ref
	spec        runtime.Typed
	base        v1.ReadWriteOCMRepositoryPluginContract[runtime.Typed]
	credentials map[string]string
}

func New(ctx context.Context, manager *manager.PluginManager, graph *credentials.Graph, componentReference string) (*ComponentRepository, error) {
	ref, err := compref.Parse(componentReference)
	if err != nil {
		return nil, fmt.Errorf("parsing component reference %q failed: %w", componentReference, err)
	}

	repositorySpec := ref.Repository
	plugin, err := manager.ComponentVersionRepositoryRegistry.GetPlugin(ctx, repositorySpec)
	if err != nil {
		return nil, fmt.Errorf("getting plugin for repository %q failed: %w", repositorySpec, err)
	}
	var creds map[string]string
	identity, err := plugin.GetIdentity(ctx, v1.GetIdentityRequest[runtime.Typed]{Typ: repositorySpec})
	if err == nil {
		if creds, err = graph.Resolve(ctx, identity); err != nil {
			return nil, fmt.Errorf("getting credentials for repository %q failed: %w", repositorySpec, err)
		}
	}
	return &ComponentRepository{
		ref:         ref,
		spec:        repositorySpec,
		base:        plugin,
		credentials: creds,
	}, nil
}

func (repo *ComponentRepository) ComponentReference() *compref.Ref {
	return repo.ref
}

type GetComponentVersionsOptions struct {
	VersionOptions
	ConcurrencyLimit int
}

func (repo *ComponentRepository) GetComponentVersions(ctx context.Context, opts GetComponentVersionsOptions) ([]*descriptor.Descriptor, error) {
	versions, err := repo.Versions(ctx, opts.VersionOptions)
	if err != nil {
		return nil, fmt.Errorf("getting component versions failed: %w", err)
	}

	descs := make([]*descriptor.Descriptor, len(versions))
	var descMu sync.Mutex

	eg, ctx := errgroup.WithContext(ctx)
	eg.SetLimit(opts.ConcurrencyLimit)
	for i, version := range versions {
		eg.Go(func() error {
			desc, err := repo.base.GetComponentVersion(ctx, v1.GetComponentVersionRequest[runtime.Typed]{
				Repository: repo.spec,
				Name:       repo.ref.Component,
				Version:    version,
			}, repo.credentials)
			if err != nil {
				return fmt.Errorf("getting component version failed: %w", err)
			}

			descMu.Lock()
			defer descMu.Unlock()
			descs[i] = desc

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("getting component versions failed: %w", err)
	}

	return descs, nil
}

type VersionOptions struct {
	SemverConstraint string
}

func (repo *ComponentRepository) Versions(ctx context.Context, opts VersionOptions) ([]string, error) {
	if repo.ref.Version != "" {
		return []string{repo.ref.Version}, nil
	}

	versions, err := repo.base.ListComponentVersions(ctx, v1.ListComponentVersionsRequest[runtime.Typed]{
		Repository: repo.spec,
		Name:       repo.ref.Component,
	}, repo.credentials)
	if err != nil {
		return nil, fmt.Errorf("listing component versions failed: %w", err)
	}

	if opts.SemverConstraint != "" {
		if versions, err = filterBySemver(versions, repo.ref.Version); err != nil {
			return nil, fmt.Errorf("filtering component versions failed: %w", err)
		}
	}

	return versions, nil
}

func filterBySemver(versions []string, constraint string) ([]string, error) {
	filteredVersions := make([]string, 0, len(versions))
	constraints, err := semver.NewConstraint(constraint)
	if err != nil {
		return nil, fmt.Errorf("parsing semantic version constraint failed: %w", err)
	}
	for _, version := range versions {
		semversion, err := semver.NewVersion(version)
		if err != nil {
			continue
		}
		if !constraints.Check(semversion) {
			continue
		}
		filteredVersions = append(filteredVersions, version)
	}
	return filteredVersions, nil
}
