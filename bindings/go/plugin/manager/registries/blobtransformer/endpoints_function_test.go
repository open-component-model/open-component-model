package blobtransformer

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/blobtransformer/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockPlugin struct {
	contracts.EmptyBasePlugin
}

func (m *mockPlugin) TransformBlob(_ context.Context, _ *v1.TransformBlobRequest[*dummyv1.Repository], _ map[string]string) (*v1.TransformBlobResponse, error) {
	return &v1.TransformBlobResponse{}, nil
}

func (m *mockPlugin) GetIdentity(_ context.Context, _ *v1.GetIdentityRequest[*dummyv1.Repository]) (*v1.GetIdentityResponse, error) {
	return &v1.GetIdentityResponse{}, nil
}

var _ v1.BlobTransformerPluginContract[*dummyv1.Repository] = &mockPlugin{}

func TestRegisterBlobTransformer(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	builder := endpoints.NewEndpoints(scheme)
	proto := &dummyv1.Repository{}
	plugin := &mockPlugin{}
	r.NoError(RegisterBlobTransformer(proto, plugin, builder))
	rawPluginSpec, err := pluginruntime.ConvertToSpec(&builder.PluginSpec)
	r.NoError(err)
	// Validate registered types
	content, err := json.Marshal(rawPluginSpec)
	r.Contains(string(content), "blobTransformer")

	handlers := builder.GetHandlers()
	r.Len(handlers, 2)
	r.Equal(TransformBlob, handlers[0].Location)
	r.Equal(Identity, handlers[1].Location)

	capabilityList := builder.PluginSpec.CapabilitySpecs
	r.Len(capabilityList, 1)
	capability := v1.CapabilitySpec{}
	r.NoError(v1.Scheme.Convert(capabilityList[0], &capability))
	typeInfo := capability.SupportedTransformerSpecTypes[0]
	r.Equal("DummyRepository/v1", typeInfo.Type.String())
	r.NotEmpty(typeInfo.JSONSchema)
}
