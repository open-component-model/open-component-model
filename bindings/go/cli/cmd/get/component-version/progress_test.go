package componentversion

import (
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/internal/render/progress"
	"ocm.software/open-component-model/bindings/go/cli/internal/render/progress/bar"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

// newTestTracker builds a tracker over a non-terminal writer, so it uses the
// slog fallback. Its events therefore appear as slog lines, which the test
// captures with captureLogs.
func newTestTracker(t *testing.T) *progress.Tracker[string] {
	t.Helper()
	return progress.NewTracker(t.Context(), io.Discard, bar.NewVisualizer[string])
}

func TestCollectComponentVersions_EmitsProgressPerComponent(t *testing.T) {
	logs := captureLogs(t)
	tracker := newTestTracker(t)

	resolver := &fakeResolver{
		repos: map[string]repository.ComponentVersionRepository{
			"comp/a": &fakeCVRepo{versions: []string{"1.0.0"}},
		},
		errs: map[string]error{"bad/one": &unauthorizedError{}},
	}

	entries, err := collectComponentVersions(t.Context(), tracker, resolver,
		[]string{"comp/a", "bad/one"}, Params{}, versioning.Default())
	tracker.Stop()
	require.NoError(t, err)
	require.Len(t, entries, 1)

	out := logs.String()
	assert.Contains(t, out, "Listing component versions: item completed")
	assert.Contains(t, out, "Listing component versions: item failed")
	assert.Contains(t, out, "bad/one")
}

func TestGetIDsForComponentsFromRepository_EmitsProgressInBothPhases(t *testing.T) {
	logs := captureLogs(t)
	tracker := newTestTracker(t)

	resolver := &fakeResolver{
		repos: map[string]repository.ComponentVersionRepository{
			"comp/good":     &fakeCVRepo{versions: []string{"1.0.0"}},
			"comp/badfetch": &fakeCVRepo{versions: []string{"1.0.0"}, getErr: &unauthorizedError{}},
		},
	}

	roots, err := getIDsForComponentsFromRepository(t.Context(), tracker, resolver,
		[]string{"comp/good", "comp/badfetch"}, Params{}, versioning.Default())
	tracker.Stop()
	require.NoError(t, err)
	require.Len(t, roots, 1, "only the readable component becomes a root")

	out := logs.String()
	// Phase 1 ran for both components.
	assert.Contains(t, out, "Listing component versions: item completed")
	// Phase 2 ran, with a failed fetch for the unreadable component.
	assert.Contains(t, out, "Fetching component descriptors: item completed")
	assert.Contains(t, out, "Fetching component descriptors: item failed")
	assert.Contains(t, out, "comp/badfetch:1.0.0")
}
