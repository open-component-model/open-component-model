package componentversion

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
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
}

func (f *fakeCVRepo) ListComponentVersions(_ context.Context, _ string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.versions, nil
}

func (f *fakeCVRepo) GetComponentVersion(_ context.Context, _, _ string) (*descriptor.Descriptor, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.descriptor, nil
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
		Params{latestOnly: true}, nil)
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
		[]string{"a", "b"}, Params{latestOnly: true}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "a:")
	assert.Contains(t, err.Error(), "b:")
	assert.ErrorIs(t, err, ocidescriptor.ErrUnsupportedDescriptorSchema)
}

// unauthorizedError stands in for a 401/403 pull error.
type unauthorizedError struct{}

func (*unauthorizedError) Error() string { return "401 unauthorized" }
