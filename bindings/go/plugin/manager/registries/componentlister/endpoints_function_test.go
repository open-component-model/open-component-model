package componentlister

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/componentlister/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockPlugin struct {
	contracts.EmptyBasePlugin
}

func (m *mockPlugin) ListComponents(ctx context.Context, request *v1.ListComponentsRequest[*dummyv1.Repository], credentials map[string]string) (*v1.ListComponentsResponse, error) {
	return &v1.ListComponentsResponse{}, nil
}

func (m *mockPlugin) GetIdentity(ctx context.Context, typ *v1.GetIdentityRequest[*dummyv1.Repository]) (*v1.GetIdentityResponse, error) {
	panic("not implemented")
}

var _ v1.ComponentListerPluginContract[*dummyv1.Repository] = &mockPlugin{}

func TestRegisterComponentLister(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	builder := endpoints.NewEndpoints(scheme)
	typ := &dummyv1.Repository{}
	plugin := &mockPlugin{}
	r.NoError(RegisterComponentLister(typ, plugin, builder))

	rawPluginSpec, err := pluginruntime.ConvertToSpec(&builder.PluginSpec)
	r.NoError(err)
	// Validate registered types
	content, err := json.Marshal(rawPluginSpec)
	r.NoError(err)
	r.Contains(string(content), "componentLister")

	handlers := builder.GetHandlers()
	r.Len(handlers, 2)
	r.Equal(ListComponents, handlers[0].Location)
	r.Equal(Identity, handlers[1].Location)

	capabilityList := builder.PluginSpec.CapabilitySpecs
	r.Len(capabilityList, 1)
	capability := v1.CapabilitySpec{}
	r.NoError(v1.Scheme.Convert(capabilityList[0], &capability))
	typeInfo := capability.SupportedRepositorySpecTypes[0]
	r.Equal("DummyRepository/v1", typeInfo.Type.String())
	r.NotEmpty(typeInfo.JSONSchema)
}
