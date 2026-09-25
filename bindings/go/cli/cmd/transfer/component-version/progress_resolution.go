package component_version

import (
	"context"

	"ocm.software/open-component-model/bindings/go/cli/internal/render/progress"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	graphPkg "ocm.software/open-component-model/bindings/go/transform/graph"
)

// resolutionEvent reports the state of a single component version lookup during graph
// construction (the "Resolving component versions" operation). Because references are
// discovered recursively, the total number of lookups is not known up front, so the
// operation is tracked with [progress.IndeterminateTotal].
type resolutionEvent struct {
	// id is "component:version", or only the component name when its versions are listed.
	id    string
	state progress.State
	err   error
}

// mapResolutionEvent converts a resolution event to a typed progress.Event.
func mapResolutionEvent(e resolutionEvent) progress.Event[*graphPkg.Transformation] {
	return progress.Event[*graphPkg.Transformation]{
		ID:    e.id,
		Name:  e.id,
		State: e.state,
		Err:   e.err,
	}
}

// resolutionProgressResolver wraps a [resolvers.ComponentVersionRepositoryResolver] and
// reports every lookup performed through it to the events channel, so the CLI can show
// progress while the transfer graph is constructed.
type resolutionProgressResolver struct {
	resolvers.ComponentVersionRepositoryResolver
	events chan<- resolutionEvent
}

var _ resolvers.ComponentVersionRepositoryResolver = (*resolutionProgressResolver)(nil)

func (r *resolutionProgressResolver) GetComponentVersionRepositoryForComponent(ctx context.Context, component, version string) (repository.ComponentVersionRepository, error) {
	repo, err := r.ComponentVersionRepositoryResolver.GetComponentVersionRepositoryForComponent(ctx, component, version)
	if err != nil {
		return nil, err
	}
	return &resolutionProgressRepository{ComponentVersionRepository: repo, events: r.events}, nil
}

func (r *resolutionProgressResolver) GetComponentVersionRepositoryForSpecification(ctx context.Context, specification runtime.Typed) (repository.ComponentVersionRepository, error) {
	repo, err := r.ComponentVersionRepositoryResolver.GetComponentVersionRepositoryForSpecification(ctx, specification)
	if err != nil {
		return nil, err
	}
	return &resolutionProgressRepository{ComponentVersionRepository: repo, events: r.events}, nil
}

// resolutionProgressRepository reports GetComponentVersion and ListComponentVersions calls
// as progress events. Those are the network-bound source lookups of graph construction.
// Write operations (Add*) are delegated without reporting: they only happen on target
// repositories, which are tracked by the transfer phase's own transformation progress.
type resolutionProgressRepository struct {
	repository.ComponentVersionRepository
	events chan<- resolutionEvent
}

var _ repository.ComponentVersionRepository = (*resolutionProgressRepository)(nil)

func (r *resolutionProgressRepository) GetComponentVersion(ctx context.Context, component, version string) (*descriptor.Descriptor, error) {
	id := component + ":" + version
	r.events <- resolutionEvent{id: id, state: progress.Running}
	desc, err := r.ComponentVersionRepository.GetComponentVersion(ctx, component, version)
	r.events <- resolutionEvent{id: id, state: resolutionState(err), err: err}
	return desc, err
}

func (r *resolutionProgressRepository) ListComponentVersions(ctx context.Context, component string) ([]string, error) {
	r.events <- resolutionEvent{id: component, state: progress.Running}
	versions, err := r.ComponentVersionRepository.ListComponentVersions(ctx, component)
	r.events <- resolutionEvent{id: component, state: resolutionState(err), err: err}
	return versions, err
}

func resolutionState(err error) progress.State {
	if err != nil {
		return progress.Failed
	}
	return progress.Completed
}
