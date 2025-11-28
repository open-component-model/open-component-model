package resource

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/resource/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockPlugin struct {
	contracts.EmptyBasePlugin
}

func (m mockPlugin) GetIdentity(ctx context.Context, request *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error) {
	return nil, nil
}

func (m mockPlugin) GetGlobalResource(ctx context.Context, request *v1.GetGlobalResourceRequest, credentials map[string]string) (*v1.GetGlobalResourceResponse, error) {
	return nil, nil
}

func (m mockPlugin) AddGlobalResource(ctx context.Context, request *v1.AddGlobalResourceRequest, credentials map[string]string) (*v1.AddGlobalResourceResponse, error) {
	return nil, nil
}

var _ v1.ReadWriteResourcePluginContract = &mockPlugin{}

func TestRegisterResourcePlugin(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	builder := endpoints.NewEndpoints(scheme)
	typ := &dummyv1.Repository{}
	plugin := &mockPlugin{}
	r.NoError(RegisterResourcePlugin(typ, plugin, builder))

	rawPluginSpec, err := pluginruntime.ConvertToSpec(&builder.PluginSpec)
	r.NoError(err)

	content, err := json.Marshal(rawPluginSpec)
	r.NoError(err)
	r.Equal(`{"capabilities":[{"supportedAccessTypes":[{"aliases":null,"jsonSchema":"eyIkc2NoZW1hIjoiaHR0cHM6Ly9qc29uLXNjaGVtYS5vcmcvZHJhZnQvMjAyMC0xMi9zY2hlbWEiLCIkaWQiOiJodHRwczovL29jbS5zb2Z0d2FyZS9vcGVuLWNvbXBvbmVudC1tb2RlbC9iaW5kaW5ncy9nby9wbHVnaW4vaW50ZXJuYWwvZHVtbXl0eXBlL3YxL3JlcG9zaXRvcnkiLCIkcmVmIjoiIy8kZGVmcy9SZXBvc2l0b3J5IiwiJGRlZnMiOnsiUmVwb3NpdG9yeSI6eyJwcm9wZXJ0aWVzIjp7InR5cGUiOnsidHlwZSI6InN0cmluZyIsInBhdHRlcm4iOiJeKFthLXpBLVowLTldW2EtekEtWjAtOS5dKikoPzovKHZbMC05XSsoPzphbHBoYVswLTldK3xiZXRhWzAtOV0rKT8pKT8ifSwiYmFzZVVybCI6eyJ0eXBlIjoic3RyaW5nIn19LCJhZGRpdGlvbmFsUHJvcGVydGllcyI6ZmFsc2UsInR5cGUiOiJvYmplY3QiLCJyZXF1aXJlZCI6WyJ0eXBlIiwiYmFzZVVybCJdfX19","type":"DummyRepository/v1"}],"type":"resourceRepository"}]}`, string(content))

	handlers := builder.GetHandlers()
	r.Len(handlers, 3)
	handler0 := handlers[0]
	handler1 := handlers[1]
	handler2 := handlers[2]

	r.Equal(GetIdentity, handler0.Location)
	r.Equal(GetGlobalResource, handler1.Location)
	r.Equal(AddGlobalResource, handler2.Location)
}
