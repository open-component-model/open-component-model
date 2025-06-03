package inputrepository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/input/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockPlugin struct {
	contracts.EmptyBasePlugin
}

func (m *mockPlugin) ProcessResource(ctx context.Context, request *v1.ProcessResourceInputRequest, credentials map[string]string) (*v1.ProcessResourceResponse, error) {
	return nil, nil
}

func (m *mockPlugin) ProcessSource(ctx context.Context, request *v1.ProcessSourceInputRequest, credentials map[string]string) (*v1.ProcessSourceResponse, error) {
	return nil, nil
}

func (m *mockPlugin) GetIdentity(ctx context.Context, typ *v1.GetIdentityRequest[runtime.Typed]) (runtime.Identity, error) {
	return nil, nil
}

var _ v1.InputPluginContract = &mockPlugin{}

func TestRegisterInputProcessor(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	builder := endpoints.NewEndpoints(scheme)
	typ := &dummyv1.Repository{}
	plugin := &mockPlugin{}
	r.NoError(RegisterInputProcessor(typ, plugin, builder))
	content, err := json.Marshal(builder)
	r.NoError(err)
	r.Equal(`{"types":{"inputRepository":[{"type":"DummyRepository/v1","jsonSchema":"eyIkc2NoZW1hIjoiaHR0cHM6Ly9qc29uLXNjaGVtYS5vcmcvZHJhZnQvMjAyMC0xMi9zY2hlbWEiLCIkaWQiOiJodHRwczovL29jbS5zb2Z0d2FyZS9vcGVuLWNvbXBvbmVudC1tb2RlbC9iaW5kaW5ncy9nby9wbHVnaW4vaW50ZXJuYWwvZHVtbXl0eXBlL3YxL3JlcG9zaXRvcnkiLCIkcmVmIjoiIy8kZGVmcy9SZXBvc2l0b3J5IiwiJGRlZnMiOnsiUmVwb3NpdG9yeSI6eyJwcm9wZXJ0aWVzIjp7InR5cGUiOnsidHlwZSI6InN0cmluZyIsInBhdHRlcm4iOiJeKFthLXpBLVowLTldW2EtekEtWjAtOS5dKikoPzovKHZbMC05XSsoPzphbHBoYVswLTldK3xiZXRhWzAtOV0rKT8pKT8ifSwiYmFzZVVybCI6eyJ0eXBlIjoic3RyaW5nIn19LCJhZGRpdGlvbmFsUHJvcGVydGllcyI6ZmFsc2UsInR5cGUiOiJvYmplY3QiLCJyZXF1aXJlZCI6WyJ0eXBlIiwiYmFzZVVybCJdfX19"}]}}`, string(content))

	handlers := builder.GetHandlers()
	r.Len(handlers, 2)
	handler0 := handlers[0]
	handler1 := handlers[1]

	r.Equal(ProcessResource, handler0.Location)
	r.Equal(ProcessSource, handler1.Location)
}
