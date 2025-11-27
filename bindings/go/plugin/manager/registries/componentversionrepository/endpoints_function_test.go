package componentversionrepository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"

	"ocm.software/open-component-model/bindings/go/plugin/manager/types"

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

func (m *mockPlugin) AddLocalSource(_ context.Context, _ repov1.PostLocalSourceRequest[*dummyv1.Repository], _ map[string]string) (*descriptor.Source, error) {
	return &descriptor.Source{}, nil
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

func (m *mockPlugin) ListComponentVersions(_ context.Context, _ repov1.ListComponentVersionsRequest[*dummyv1.Repository], _ map[string]string) ([]string, error) {
	return []string{"v0.0.1", "v0.0.2"}, nil
}

func (m *mockPlugin) GetLocalResource(_ context.Context, _ repov1.GetLocalResourceRequest[*dummyv1.Repository], _ map[string]string) (repov1.GetLocalResourceResponse, error) {
	return repov1.GetLocalResourceResponse{
		Location: types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        "/dummy/local-file",
		},
		Resource: &v2.Resource{
			ElementMeta: v2.ElementMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "test-resource",
					Version: "v0.0.1",
				},
			},
			Type:     "resource-type",
			Relation: "local",
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    "test-access",
					Version: "v1",
				},
				Data: []byte(`{ "access": "v1" }`),
			},
			Digest: &v2.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "jsonNormalisation/v1",
				Value:                  "test-value",
			},
		},
	}, nil
}

func (m *mockPlugin) GetLocalSource(_ context.Context, _ repov1.GetLocalSourceRequest[*dummyv1.Repository], _ map[string]string) (repov1.GetLocalSourceResponse, error) {
	return repov1.GetLocalSourceResponse{
		Location: types.Location{
			LocationType: types.LocationTypeLocalFile,
			Value:        "/dummy/local-file",
		},
		Source: &v2.Source{
			ElementMeta: v2.ElementMeta{
				ObjectMeta: v2.ObjectMeta{
					Name:    "test-source",
					Version: "v0.0.1",
				},
			},
			Type: "source-type",
			Access: &runtime.Raw{
				Type: runtime.Type{
					Name:    "test-access",
					Version: "v1",
				},
				Data: []byte(`{ "access": "v1" }`),
			},
		},
	}, nil
}

func (m *mockPlugin) GetIdentity(ctx context.Context, typ *repov1.GetIdentityRequest[*dummyv1.Repository]) (*repov1.GetIdentityResponse, error) {
	return nil, nil
}

func (m *mockPlugin) CheckHealth(ctx context.Context, typ repov1.PostCheckHealthRequest[*dummyv1.Repository], credentials map[string]string) error {
	return nil
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

	rawPluginSpec, err := pluginruntime.ConvertToSpec(&builder.PluginSpec)
	r.NoError(err)
	// Validate registered types
	content, err := json.Marshal(rawPluginSpec)
	r.NoError(err)
	r.Equal(`{"capabilities":[{"supportedRepositorySpecTypes":[{"aliases":null,"jsonSchema":"eyIkc2NoZW1hIjoiaHR0cHM6Ly9qc29uLXNjaGVtYS5vcmcvZHJhZnQvMjAyMC0xMi9zY2hlbWEiLCIkaWQiOiJodHRwczovL29jbS5zb2Z0d2FyZS9vcGVuLWNvbXBvbmVudC1tb2RlbC9iaW5kaW5ncy9nby9wbHVnaW4vaW50ZXJuYWwvZHVtbXl0eXBlL3YxL3JlcG9zaXRvcnkiLCIkcmVmIjoiIy8kZGVmcy9SZXBvc2l0b3J5IiwiJGRlZnMiOnsiUmVwb3NpdG9yeSI6eyJwcm9wZXJ0aWVzIjp7InR5cGUiOnsidHlwZSI6InN0cmluZyIsInBhdHRlcm4iOiJeKFthLXpBLVowLTldW2EtekEtWjAtOS5dKikoPzovKHZbMC05XSsoPzphbHBoYVswLTldK3xiZXRhWzAtOV0rKT8pKT8ifSwiYmFzZVVybCI6eyJ0eXBlIjoic3RyaW5nIn19LCJhZGRpdGlvbmFsUHJvcGVydGllcyI6ZmFsc2UsInR5cGUiOiJvYmplY3QiLCJyZXF1aXJlZCI6WyJ0eXBlIiwiYmFzZVVybCJdfX19","type":"DummyRepository/v1"}],"type":"componentVersionRepository"}]}`, string(content))

	handlers := builder.GetHandlers()
	r.Len(handlers, 8)
	handler0 := handlers[0]
	handler1 := handlers[1]
	handler2 := handlers[2]
	handler3 := handlers[3]
	handler4 := handlers[4]
	handler5 := handlers[5]
	handler6 := handlers[6]
	handler7 := handlers[7]

	r.Equal(DownloadComponentVersion, handler0.Location)
	r.Equal(ListComponentVersions, handler1.Location)
	r.Equal(DownloadLocalResource, handler2.Location)
	r.Equal(UploadComponentVersion, handler3.Location)
	r.Equal(UploadLocalResource, handler4.Location)
	r.Equal(Identity, handler5.Location)
	r.Equal(UploadLocalSource, handler6.Location)
	r.Equal(DownloadLocalSource, handler7.Location)
}
