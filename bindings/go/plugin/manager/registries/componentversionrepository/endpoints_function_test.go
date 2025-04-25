package componentversionrepository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockPlugin struct {
	contracts.EmptyBasePlugin
}

func (m *mockPlugin) AddLocalResource(_ context.Context, _ types.PostLocalResourceRequest[*v1.OCIRepository], _ contracts.Attributes) (*descriptor.Resource, error) {
	return &descriptor.Resource{}, nil
}

func (m *mockPlugin) AddComponentVersion(_ context.Context, _ types.PostComponentVersionRequest[*v1.OCIRepository], _ contracts.Attributes) error {
	return nil
}

func (m *mockPlugin) GetComponentVersion(_ context.Context, _ types.GetComponentVersionRequest[*v1.OCIRepository], _ contracts.Attributes) (*descriptor.Descriptor, error) {
	return &descriptor.Descriptor{}, nil
}

func (m *mockPlugin) GetLocalResource(_ context.Context, _ types.GetLocalResourceRequest[*v1.OCIRepository], _ contracts.Attributes) error {
	return nil
}

var _ contracts.ReadWriteOCMRepositoryPluginContract[*v1.OCIRepository] = &mockPlugin{}

func TestRegisterComponentVersionRepository(t *testing.T) {
	r := require.New(t)

	scheme := runtime.NewScheme()
	repository.MustAddToScheme(scheme)
	builder := endpoints.NewEndpoints(scheme)
	typ := &v1.OCIRepository{}
	plugin := &mockPlugin{}
	r.NoError(RegisterComponentVersionRepository(typ, plugin, builder))
	content, err := json.Marshal(builder)
	r.NoError(err)
	r.Equal("{\"types\":{\"componentVersionRepository\":[{\"type\":\"OCIRepository/v1\",\"jsonSchema\":\"eyIkc2NoZW1hIjoiaHR0cHM6Ly9qc29uLXNjaGVtYS5vcmcvZHJhZnQvMjAyMC0xMi9zY2hlbWEiLCIkaWQiOiJodHRwczovL29jbS5zb2Z0d2FyZS9vcGVuLWNvbXBvbmVudC1tb2RlbC9iaW5kaW5ncy9nby9vY2kvc3BlYy9yZXBvc2l0b3J5L3YxL29jaS1yZXBvc2l0b3J5IiwiJHJlZiI6IiMvJGRlZnMvT0NJUmVwb3NpdG9yeSIsIiRkZWZzIjp7Ik9DSVJlcG9zaXRvcnkiOnsicHJvcGVydGllcyI6eyJ0eXBlIjp7IiRyZWYiOiIjLyRkZWZzL1R5cGUifSwiYmFzZVVybCI6eyJ0eXBlIjoic3RyaW5nIn19LCJhZGRpdGlvbmFsUHJvcGVydGllcyI6ZmFsc2UsInR5cGUiOiJvYmplY3QiLCJyZXF1aXJlZCI6WyJ0eXBlIiwiYmFzZVVybCJdfSwiVHlwZSI6eyJwcm9wZXJ0aWVzIjp7IlZlcnNpb24iOnsidHlwZSI6InN0cmluZyJ9LCJOYW1lIjp7InR5cGUiOiJzdHJpbmcifX0sImFkZGl0aW9uYWxQcm9wZXJ0aWVzIjpmYWxzZSwidHlwZSI6Im9iamVjdCIsInJlcXVpcmVkIjpbIlZlcnNpb24iLCJOYW1lIl19fX0=\"}]}}", string(content))

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
