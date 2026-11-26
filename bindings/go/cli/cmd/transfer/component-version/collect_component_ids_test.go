package component_version

import (
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
)

// fakeCVRepo implements the one repository.ComponentVersionRepository method
// that version listing needs. The embedded interface is nil.
type fakeCVRepo struct {
	repository.ComponentVersionRepository
	versions []string
	listErr  error
}

func (f *fakeCVRepo) ListComponentVersions(_ context.Context, _ string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.versions, nil
}

// fakeResolver returns a preconfigured repository (or error) per component.
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

func newTransferFlags() *cobra.Command {
	c := &cobra.Command{}
	c.Flags().String(FlagConstraint, "", "")
	c.Flags().Bool(FlagLatest, true, "")
	return c
}

func TestCollectComponentIDs_RepositorySourceFailsNamingUnreadable(t *testing.T) {
	resolver := &fakeResolver{
		repos: map[string]repository.ComponentVersionRepository{
			"ok/component": &fakeCVRepo{versions: []string{"v1.0.0"}},
		},
		errs: map[string]error{
			"broken/component": &unauthorizedError{},
		},
	}

	ids, err := collectComponentIDs(t.Context(), newTransferFlags(), nil, resolver,
		&compref.Ref{}, []string{"ok/component", "broken/component"})

	// A whole-registry transfer must not start when a component cannot be read.
	require.Error(t, err)
	assert.Nil(t, ids)
	assert.Contains(t, err.Error(), "broken/component")
	assert.Contains(t, err.Error(), "could not be read")
	assert.Contains(t, err.Error(), "no transfer was started")
}

func TestCollectComponentIDs_SingleComponentIsHardFailure(t *testing.T) {
	resolver := &fakeResolver{
		errs: map[string]error{"ok/component": &unauthorizedError{}},
	}

	// sourceComponents nil => single-component source; the error is immediate.
	_, err := collectComponentIDs(t.Context(), newTransferFlags(), nil, resolver,
		&compref.Ref{Component: "ok/component"}, nil)
	require.Error(t, err)
}

// unauthorizedError stands in for a 401/403 access error on version listing.
type unauthorizedError struct{}

func (*unauthorizedError) Error() string { return "401 unauthorized" }
