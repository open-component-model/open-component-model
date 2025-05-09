package componentversionrepository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	repov1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/ocmrepository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockPlugin struct {
	contracts.EmptyBasePlugin
}

func (m *mockPlugin) AddLocalResource(_ context.Context, _ repov1.PostLocalResourceRequest[*dummyv1.Repository], _ map[string]string) (*descriptor.Resource, error) {
	return &descriptor.Resource{}, nil
}

func (m *mockPlugin) AddComponentVersion(_ context.Context, _ repov1.PostComponentVersionRequest[*dummyv1.Repository], _ map[string]string) error {
	return nil
}

func (m *mockPlugin) GetComponentVersion(_ context.Context, _ repov1.GetComponentVersionRequest[*dummyv1.Repository], _ map[string]string) (*descriptor.Descriptor, error) {
	return &descriptor.Descriptor{
		Meta: descriptor.Meta{
			Version: "1.0.0",
		},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{
					Name:    "test-mock-component",
					Version: "v1.0.0",
				},
			},
		},
	}, nil
}

func (m *mockPlugin) GetLocalResource(_ context.Context, _ repov1.GetLocalResourceRequest[*dummyv1.Repository], _ map[string]string) error {
	return nil
}

func (m *mockPlugin) GetIdentity(ctx context.Context, typ repov1.GetIdentityRequest[*dummyv1.Repository]) (runtime.Identity, error) {
	return nil, nil
}

var _ repov1.ReadWriteOCMRepositoryPluginContract[*dummyv1.Repository] = &mockPlugin{}

func TestRegisterComponentVersionRepository(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	builder := endpoints.NewEndpoints(scheme)
	typ := &dummyv1.Repository{}
	plugin := &mockPlugin{}
	r.NoError(RegisterComponentVersionRepository(typ, plugin, builder))
	content, err := json.Marshal(builder)
	r.NoError(err)
	r.Equal(`{"types":{"componentVersionRepository":[{"type":"DummyRepository/v1","jsonSchema":"eyIkc2NoZW1hIjoiaHR0cHM6Ly9qc29uLXNjaGVtYS5vcmcvZHJhZnQvMjAyMC0xMi9zY2hlbWEiLCIkaWQiOiJodHRwczovL29jbS5zb2Z0d2FyZS9vcGVuLWNvbXBvbmVudC1tb2RlbC9iaW5kaW5ncy9nby9wbHVnaW4vaW50ZXJuYWwvZHVtbXl0eXBlL3YxL3JlcG9zaXRvcnkiLCIkcmVmIjoiIy8kZGVmcy9SZXBvc2l0b3J5IiwiJGRlZnMiOnsiUmVwb3NpdG9yeSI6eyJwcm9wZXJ0aWVzIjp7InR5cGUiOnsidHlwZSI6InN0cmluZyIsInBhdHRlcm4iOiJeKFthLXpBLVowLTldW2EtekEtWjAtOS5dKikoPzovKHZbMC05XSsoPzphbHBoYVswLTldK3xiZXRhWzAtOV0rKT8pKT8ifSwiYmFzZVVybCI6eyJ0eXBlIjoic3RyaW5nIn19LCJhZGRpdGlvbmFsUHJvcGVydGllcyI6ZmFsc2UsInR5cGUiOiJvYmplY3QiLCJyZXF1aXJlZCI6WyJ0eXBlIiwiYmFzZVVybCJdfX19"}]}}`, string(content))

	handlers := builder.GetHandlers()
	r.Len(handlers, 4)
	handler0 := handlers[0]
	handler1 := handlers[1]
	handler2 := handlers[2]
	handler3 := handlers[3]

	r.Equal(DownloadComponentVersion, handler0.Location)
	r.Equal(DownloadLocalResource, handler1.Location)
	r.Equal(UploadComponentVersion, handler2.Location)
	r.Equal(UploadLocalResource, handler3.Location)
}
