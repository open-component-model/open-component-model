package componentlister

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"

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

func (m *mockPlugin) ListComponents(ctx context.Context, request v1.ListComponentsRequest[*dummyv1.Repository], credentials map[string]string) ([]string, error) {
	return []string{}, nil
}

func (m *mockPlugin) GetIdentity(ctx context.Context, typ *v1.GetIdentityRequest[*dummyv1.Repository]) (*v1.GetIdentityResponse, error) {
	return nil, nil
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
	content, err := json.Marshal(builder)
	r.NoError(err)
	r.Contains(string(content), "componentLister")

	handlers := builder.GetHandlers()
	r.Len(handlers, 2)
	r.Equal(ListComponents, handlers[0].Location)
	r.Equal(Identity, handlers[1].Location)

	typesList := builder.CurrentTypes.Types[types.ComponentListerPluginType]
	r.Len(typesList, 1)
	r.Equal("DummyRepository/v1", typesList[0].Type.String())
	r.NotEmpty(typesList[0].JSONSchema)
}
