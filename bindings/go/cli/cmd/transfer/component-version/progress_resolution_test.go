package component_version

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/internal/render/progress"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// stubResolver implements resolvers.ComponentVersionRepositoryResolver with canned results.
type stubResolver struct {
	repo    repository.ComponentVersionRepository
	repoErr error
	spec    runtime.Typed
	specErr error
}

func (s *stubResolver) GetComponentVersionRepositoryForComponent(context.Context, string, string) (repository.ComponentVersionRepository, error) {
	return s.repo, s.repoErr
}

func (s *stubResolver) GetComponentVersionRepositoryForSpecification(context.Context, runtime.Typed) (repository.ComponentVersionRepository, error) {
	return s.repo, s.repoErr
}

func (s *stubResolver) GetRepositorySpecificationForComponent(context.Context, string, string) (runtime.Typed, error) {
	return s.spec, s.specErr
}

// stubRepository embeds the repository interface and only implements the lookups the wrapper reports on.
type stubRepository struct {
	repository.ComponentVersionRepository
	desc     *descriptor.Descriptor
	versions []string
	err      error
}

func (s *stubRepository) GetComponentVersion(context.Context, string, string) (*descriptor.Descriptor, error) {
	return s.desc, s.err
}

func (s *stubRepository) ListComponentVersions(context.Context, string) ([]string, error) {
	return s.versions, s.err
}

func TestMapResolutionEvent(t *testing.T) {
	tests := []struct {
		name          string
		input         resolutionEvent
		expectedID    string
		expectedState progress.State
		expectedErr   error
	}{
		{
			name:          "running",
			input:         resolutionEvent{id: "ocm.software/a:1.0.0", state: progress.Running},
			expectedID:    "ocm.software/a:1.0.0",
			expectedState: progress.Running,
		},
		{
			name:          "completed",
			input:         resolutionEvent{id: "ocm.software/a:1.0.0", state: progress.Completed},
			expectedID:    "ocm.software/a:1.0.0",
			expectedState: progress.Completed,
		},
		{
			name:          "failed with error",
			input:         resolutionEvent{id: "ocm.software/a:1.0.0", state: progress.Failed, err: assert.AnError},
			expectedID:    "ocm.software/a:1.0.0",
			expectedState: progress.Failed,
			expectedErr:   assert.AnError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapResolutionEvent(tt.input)

			assert.Equal(t, tt.expectedID, result.ID)
			assert.Equal(t, tt.expectedID, result.Name)
			assert.Equal(t, tt.expectedState, result.State)
			assert.Equal(t, tt.expectedErr, result.Err)
			assert.Nil(t, result.Data)
		})
	}
}

func TestResolutionProgressResolver(t *testing.T) {
	t.Run("wraps repository resolved for component", func(t *testing.T) {
		r := require.New(t)
		events := make(chan resolutionEvent, 2)
		resolver := &resolutionProgressResolver{
			ComponentVersionRepositoryResolver: &stubResolver{repo: &stubRepository{desc: &descriptor.Descriptor{}}},
			events:                             events,
		}

		repo, err := resolver.GetComponentVersionRepositoryForComponent(t.Context(), "ocm.software/a", "1.0.0")
		r.NoError(err)

		desc, err := repo.GetComponentVersion(t.Context(), "ocm.software/a", "1.0.0")
		r.NoError(err)
		r.NotNil(desc)

		r.Equal(resolutionEvent{id: "ocm.software/a:1.0.0", state: progress.Running}, <-events)
		r.Equal(resolutionEvent{id: "ocm.software/a:1.0.0", state: progress.Completed}, <-events)
	})

	t.Run("wraps repository resolved for specification", func(t *testing.T) {
		r := require.New(t)
		events := make(chan resolutionEvent, 2)
		resolver := &resolutionProgressResolver{
			ComponentVersionRepositoryResolver: &stubResolver{repo: &stubRepository{desc: &descriptor.Descriptor{}}},
			events:                             events,
		}

		repo, err := resolver.GetComponentVersionRepositoryForSpecification(t.Context(), nil)
		r.NoError(err)

		_, err = repo.GetComponentVersion(t.Context(), "ocm.software/b", "2.0.0")
		r.NoError(err)

		r.Equal(resolutionEvent{id: "ocm.software/b:2.0.0", state: progress.Running}, <-events)
		r.Equal(resolutionEvent{id: "ocm.software/b:2.0.0", state: progress.Completed}, <-events)
	})

	t.Run("resolver errors are returned without events", func(t *testing.T) {
		r := require.New(t)
		events := make(chan resolutionEvent, 2)
		resolver := &resolutionProgressResolver{
			ComponentVersionRepositoryResolver: &stubResolver{repoErr: assert.AnError},
			events:                             events,
		}

		_, err := resolver.GetComponentVersionRepositoryForComponent(t.Context(), "ocm.software/a", "1.0.0")
		r.ErrorIs(err, assert.AnError)

		_, err = resolver.GetComponentVersionRepositoryForSpecification(t.Context(), nil)
		r.ErrorIs(err, assert.AnError)

		r.Empty(events)
	})

	t.Run("repository spec lookup is delegated without events", func(t *testing.T) {
		r := require.New(t)
		events := make(chan resolutionEvent, 1)
		resolver := &resolutionProgressResolver{
			ComponentVersionRepositoryResolver: &stubResolver{specErr: assert.AnError},
			events:                             events,
		}

		_, err := resolver.GetRepositorySpecificationForComponent(t.Context(), "ocm.software/a", "1.0.0")
		r.ErrorIs(err, assert.AnError)
		r.Empty(events)
	})
}

func TestResolutionProgressRepository(t *testing.T) {
	t.Run("get failure reports failed state", func(t *testing.T) {
		r := require.New(t)
		events := make(chan resolutionEvent, 2)
		repo := &resolutionProgressRepository{ComponentVersionRepository: &stubRepository{err: assert.AnError}, events: events}

		_, err := repo.GetComponentVersion(t.Context(), "ocm.software/a", "1.0.0")
		r.ErrorIs(err, assert.AnError)

		r.Equal(resolutionEvent{id: "ocm.software/a:1.0.0", state: progress.Running}, <-events)
		r.Equal(resolutionEvent{id: "ocm.software/a:1.0.0", state: progress.Failed, err: assert.AnError}, <-events)
	})

	t.Run("list reports success", func(t *testing.T) {
		r := require.New(t)
		events := make(chan resolutionEvent, 2)
		repo := &resolutionProgressRepository{
			ComponentVersionRepository: &stubRepository{versions: []string{"1.0.0", "2.0.0"}},
			events:                     events,
		}

		versions, err := repo.ListComponentVersions(t.Context(), "ocm.software/a")
		r.NoError(err)
		r.Equal([]string{"1.0.0", "2.0.0"}, versions)

		r.Equal(resolutionEvent{id: "ocm.software/a", state: progress.Running}, <-events)
		r.Equal(resolutionEvent{id: "ocm.software/a", state: progress.Completed}, <-events)
	})

	t.Run("list failure reports failed state", func(t *testing.T) {
		r := require.New(t)
		events := make(chan resolutionEvent, 2)
		repo := &resolutionProgressRepository{ComponentVersionRepository: &stubRepository{err: assert.AnError}, events: events}

		_, err := repo.ListComponentVersions(t.Context(), "ocm.software/a")
		r.ErrorIs(err, assert.AnError)

		r.Equal(resolutionEvent{id: "ocm.software/a", state: progress.Running}, <-events)
		r.Equal(resolutionEvent{id: "ocm.software/a", state: progress.Failed, err: assert.AnError}, <-events)
	})
}
