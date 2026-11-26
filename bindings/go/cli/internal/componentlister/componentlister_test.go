package componentlister_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/cli/internal/componentlister"
	builtinoci "ocm.software/open-component-model/bindings/go/cli/internal/plugin/builtin/oci"
	"ocm.software/open-component-model/bindings/go/credentials"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// fakeResolver is a credentials.Resolver with a fixed result. It records
// whether Resolve was called.
type fakeResolver struct {
	creds  runtime.Typed
	err    error
	called bool
}

func (f *fakeResolver) Resolve(_ context.Context, _ runtime.Identity) (runtime.Typed, error) {
	f.called = true
	return f.creds, f.err
}

func newManager(t *testing.T) *manager.PluginManager {
	t.Helper()
	pm := manager.NewPluginManager(t.Context())
	require.NoError(t, pm.ComponentListerRegistry.RegisterInternalComponentListerPlugin(
		builtinoci.NewComponentListerPlugin(nil, nil),
	))
	return pm
}

func TestResolveCredentials_CTFNeedsNone(t *testing.T) {
	pm := newManager(t)
	resolver := &fakeResolver{creds: &runtime.Raw{}}

	creds, err := componentlister.ResolveCredentials(t.Context(), pm, resolver, &ctfv1.Repository{FilePath: "x.ctf"})
	require.NoError(t, err)
	assert.Nil(t, creds)
	assert.False(t, resolver.called, "a CTF archive must not trigger credential resolution")
}

func TestResolveCredentials_OCIResolvesCredentials(t *testing.T) {
	pm := newManager(t)
	want := &runtime.Raw{}
	resolver := &fakeResolver{creds: want}

	creds, err := componentlister.ResolveCredentials(t.Context(), pm, resolver, &ociv1.Repository{BaseUrl: "ghcr.io", SubPath: "org"})
	require.NoError(t, err)
	assert.Same(t, want, creds)
	assert.True(t, resolver.called)
}

func TestResolveCredentials_OCINotFoundMeansNoCredentials(t *testing.T) {
	pm := newManager(t)
	resolver := &fakeResolver{err: credentials.ErrNotFound}

	creds, err := componentlister.ResolveCredentials(t.Context(), pm, resolver, &ociv1.Repository{BaseUrl: "ghcr.io", SubPath: "org"})
	require.NoError(t, err)
	assert.Nil(t, creds)
}

func TestResolveCredentials_OCIResolveErrorIsReturned(t *testing.T) {
	pm := newManager(t)
	sentinel := errors.New("graph failure")
	resolver := &fakeResolver{err: sentinel}

	_, err := componentlister.ResolveCredentials(t.Context(), pm, resolver, &ociv1.Repository{BaseUrl: "ghcr.io", SubPath: "org"})
	require.ErrorIs(t, err, sentinel)
}

func TestResolveCredentials_IdentityErrorIsReturned(t *testing.T) {
	pm := newManager(t)
	resolver := &fakeResolver{creds: &runtime.Raw{}}

	// An invalid base URL makes the consumer-identity lookup fail. That real
	// failure must be returned, not swallowed as "no credentials".
	_, err := componentlister.ResolveCredentials(t.Context(), pm, resolver, &ociv1.Repository{BaseUrl: "ghcr.io/%zz"})
	require.Error(t, err)
	assert.False(t, resolver.called, "credentials must not be resolved when the identity lookup fails")
}
