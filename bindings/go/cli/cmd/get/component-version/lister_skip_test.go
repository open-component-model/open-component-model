package componentversion

import (
	"bytes"
	"context"
	"log/slog"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime/versioning"
)

// fakeCVRepo implements the two repository.ComponentVersionRepository methods
// that ListComponentVersions needs. The embedded interface is nil; any other
// method would panic, which never happens in these tests.
type fakeCVRepo struct {
	repository.ComponentVersionRepository
	versions   []string
	descriptor *descriptor.Descriptor
	listErr    error
	getErr     error
	getCalled  bool
}

func (f *fakeCVRepo) ListComponentVersions(_ context.Context, _ string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.versions, nil
}

func (f *fakeCVRepo) GetComponentVersion(_ context.Context, name, version string) (*descriptor.Descriptor, error) {
	f.getCalled = true
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.descriptor != nil {
		return f.descriptor, nil
	}
	return descFor(name, version), nil
}

// fakeResolver returns a preconfigured repository (or error) per component. The
// embedded interface is nil; only GetComponentVersionRepositoryForComponent is
// used by the code under test.
type fakeResolver struct {
	resolvers.ComponentVersionRepositoryResolver
	repos map[string]repository.ComponentVersionRepository
	errs  map[string]error
}

func (f *fakeResolver) GetComponentVersionRepositoryForComponent(_ context.Context, component, _ string) (repository.ComponentVersionRepository, error) {
	if err, ok := f.errs[component]; ok {
		return nil, err
	}
	return f.repos[component], nil
}

func descFor(name, version string) *descriptor.Descriptor {
	d := &descriptor.Descriptor{}
	d.Component.Name = name
	d.Component.Version = version
	return d
}

// captureLogs redirects the default slog logger to a buffer for the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buf
}

func TestGetIDsForComponentsFromRepository_SkipsUnreadable(t *testing.T) {
	logs := captureLogs(t)

	resolver := &fakeResolver{
		repos: map[string]repository.ComponentVersionRepository{
			"ok/component":   &fakeCVRepo{versions: []string{"v1.0.0"}, descriptor: descFor("ok/component", "v1.0.0")},
			"broken/schema":  &fakeCVRepo{versions: []string{"v1.0.0"}, getErr: ocidescriptor.ErrUnsupportedDescriptorSchema},
			"empty/identity": &fakeCVRepo{versions: []string{"v1.0.0"}, descriptor: descFor("", "")},
		},
		errs: map[string]error{
			"no/access": &unauthorizedError{},
		},
	}

	roots, err := getIDsForComponentsFromRepository(t.Context(), resolver,
		[]string{"ok/component", "no/access", "broken/schema", "empty/identity"},
		Params{latestOnly: true}, versioning.Default())
	require.NoError(t, err)
	require.Len(t, roots, 1)
	assert.Contains(t, roots[0], "ok/component")

	out := logs.String()
	assert.Contains(t, out, "no/access")
	assert.Contains(t, out, "broken/schema")
	assert.Contains(t, out, "empty/identity")
	assert.Contains(t, out, "skipped 3 of 4 components")
}

func TestGetIDsForComponentsFromRepository_AllFail(t *testing.T) {
	captureLogs(t)

	resolver := &fakeResolver{
		errs: map[string]error{
			"a": &unauthorizedError{},
			"b": ocidescriptor.ErrUnsupportedDescriptorSchema,
		},
	}

	_, err := getIDsForComponentsFromRepository(t.Context(), resolver,
		[]string{"a", "b"}, Params{latestOnly: true}, versioning.Default())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a:")
	assert.Contains(t, err.Error(), "b:")
	assert.ErrorIs(t, err, ocidescriptor.ErrUnsupportedDescriptorSchema)
}

// unauthorizedError stands in for a 401/403 pull error.
type unauthorizedError struct{}

func (*unauthorizedError) Error() string { return "401 unauthorized" }

func TestGetIDsForComponentsFromRepository_UsesCustomVersionScheme(t *testing.T) {
	captureLogs(t)

	// A scheme that orders by the minor group only, so 1.5 is newer than 2.1,
	// the opposite of semver.
	scheme := versioning.NewRegexScheme("minor-only",
		regexp.MustCompile(`^(?P<x>\d+)\.(?P<y>\d+)$`), []string{"y"})
	reg := versioning.NewRegistry(scheme)

	resolver := &fakeResolver{
		repos: map[string]repository.ComponentVersionRepository{
			"comp/a": &fakeCVRepo{versions: []string{"2.1", "1.5"}},
		},
	}

	roots, err := getIDsForComponentsFromRepository(t.Context(), resolver,
		[]string{"comp/a"}, Params{}, reg)
	require.NoError(t, err)
	require.Len(t, roots, 2)

	// Under the custom scheme 1.5 (minor 5) sorts before 2.1 (minor 1).
	assert.Contains(t, roots[0], "version=1.5")
	assert.Contains(t, roots[1], "version=2.1")
}

func TestGetIDsForComponentsFromRepository_EqualVersionsOrderByNameDeterministically(t *testing.T) {
	captureLogs(t)

	resolver := &fakeResolver{
		repos: map[string]repository.ComponentVersionRepository{
			"comp/b": &fakeCVRepo{versions: []string{"1.0.0"}},
			"comp/a": &fakeCVRepo{versions: []string{"1.0.0"}},
		},
	}

	// Same version across components: the tie breaks by component name, the same
	// on every run.
	for range 5 {
		roots, err := getIDsForComponentsFromRepository(t.Context(), resolver,
			[]string{"comp/b", "comp/a"}, Params{}, versioning.Default())
		require.NoError(t, err)
		require.Len(t, roots, 2)
		assert.Contains(t, roots[0], "comp/a")
		assert.Contains(t, roots[1], "comp/b")
	}
}

func TestGetIDsForComponentsFromRepository_SortsNewestFirstAcrossComponents(t *testing.T) {
	captureLogs(t)

	resolver := &fakeResolver{
		repos: map[string]repository.ComponentVersionRepository{
			// Text sorting would order 1.10.0 before 1.9.0; the registry sort must not.
			"comp/a": &fakeCVRepo{versions: []string{"1.9.0", "1.10.0"}},
			"comp/b": &fakeCVRepo{versions: []string{"2.0.0"}},
		},
	}

	roots, err := getIDsForComponentsFromRepository(t.Context(), resolver,
		[]string{"comp/a", "comp/b"}, Params{}, versioning.Default())
	require.NoError(t, err)
	require.Len(t, roots, 3)

	// Global order is newest version first across all components.
	assert.Contains(t, roots[0], "version=2.0.0")
	assert.Contains(t, roots[1], "version=1.10.0")
	assert.Contains(t, roots[2], "version=1.9.0")
}
