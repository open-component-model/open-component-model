package componentversion

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/resource"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var testAccessType = runtime.NewVersionedType("test.ownership.access", "v1")

// testAccess is a minimal resource access spec used to drive the resource plugin
// registry to a registered internal plugin in these tests.
type testAccess struct {
	Type runtime.Type `json:"type"`
}

func (a *testAccess) GetType() runtime.Type        { return a.Type }
func (a *testAccess) SetType(t runtime.Type)       { a.Type = t }
func (a *testAccess) DeepCopyTyped() runtime.Typed { cp := *a; return &cp }

func newTestAccessScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&testAccess{}, testAccessType)
	return s
}

// basePlugin implements resource.BuiltinResourceRepository with no-op behavior. It
// stands in for an internal resource plugin that does NOT support ownership
// referrers (like an external plugin bridge).
type basePlugin struct{ scheme *runtime.Scheme }

func (p *basePlugin) GetResourceRepositoryScheme() *runtime.Scheme { return p.scheme }

func (p *basePlugin) GetResourceCredentialConsumerIdentity(context.Context, *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (p *basePlugin) DownloadResource(context.Context, *descriptor.Resource, runtime.Typed) (blob.ReadOnlyBlob, error) {
	return nil, nil
}

func (p *basePlugin) UploadResource(_ context.Context, res *descriptor.Resource, _ blob.ReadOnlyBlob, _ runtime.Typed) (*descriptor.Resource, error) {
	return res, nil
}

// ownershipPlugin additionally implements repository.OwnershipReferrerRepository,
// like the in-process OCI resource repository does, and records the delegated call.
type ownershipPlugin struct {
	basePlugin
	attachCalls  int
	gotComponent string
	gotVersion   string
}

func (p *ownershipPlugin) AddOwnershipReferrer(_ context.Context, component, version string, _ *descriptor.Resource, _ runtime.Typed) error {
	p.attachCalls++
	p.gotComponent = component
	p.gotVersion = version
	return nil
}

func providerWithPlugin(t *testing.T, plugin resource.BuiltinResourceRepository) *constructorProvider {
	t.Helper()
	reg := resource.NewResourceRegistry(t.Context())
	require.NoError(t, reg.RegisterInternalResourcePlugin(plugin))
	return &constructorProvider{pluginManager: &manager.PluginManager{ResourcePluginRegistry: reg}}
}

// TestGetResourceRepository_ForwardsOwnershipReferrer proves the CLI re-exposes the
// optional ADR-0016 capability when the resolved plugin (e.g. the in-process OCI
// resource repository) supports it: constructorPlugin.AddOwnershipReferrer must
// delegate to it, or `ocm add cv` silently warns "resource repository does not
// support ownership referrers" for OCI image resources instead of attaching one.
func TestGetResourceRepository_ForwardsOwnershipReferrer(t *testing.T) {
	plugin := &ownershipPlugin{basePlugin: basePlugin{scheme: newTestAccessScheme()}}
	prov := providerWithPlugin(t, plugin)

	repo, err := prov.GetResourceRepository(t.Context(), &constructorruntime.Resource{AccessOrInput: constructorruntime.AccessOrInput{Access: &testAccess{Type: testAccessType}}})
	require.NoError(t, err)

	attacher, ok := repo.(constructor.OwnershipReferrerAwareRepository)
	require.True(t, ok, "must forward the ownership-referrer capability for a supporting plugin")

	require.NoError(t, attacher.AddOwnershipReferrer(t.Context(), "ocm.software/test", "v1.0.0", &descriptor.Resource{}, nil))
	require.Equal(t, 1, plugin.attachCalls)
	require.Equal(t, "ocm.software/test", plugin.gotComponent)
	require.Equal(t, "v1.0.0", plugin.gotVersion)
}

// TestGetResourceRepository_SkipsOwnershipReferrerWhenUnsupported proves the
// inverse: constructorPlugin always exposes AddOwnershipReferrer, but for a plugin
// that cannot host referrers (e.g. an external plugin bridge) the call warns and
// skips — returning nil without delegating — instead of failing. That graceful
// degradation is the designed behavior for such bridges.
func TestGetResourceRepository_SkipsOwnershipReferrerWhenUnsupported(t *testing.T) {
	prov := providerWithPlugin(t, &basePlugin{scheme: newTestAccessScheme()})

	repo, err := prov.GetResourceRepository(t.Context(), &constructorruntime.Resource{AccessOrInput: constructorruntime.AccessOrInput{Access: &testAccess{Type: testAccessType}}})
	require.NoError(t, err)

	attacher, ok := repo.(constructor.OwnershipReferrerAwareRepository)
	require.True(t, ok, "constructorPlugin always exposes the optional capability")

	// The underlying plugin cannot host referrers, so the attach degrades to a
	// warn-and-skip: no delegation, no error.
	require.NoError(t, attacher.AddOwnershipReferrer(t.Context(), "ocm.software/test", "v1.0.0", &descriptor.Resource{}, nil))
}
