package credentialplugin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentialplugin/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockCredentialPluginContract[T runtime.Typed] struct {
	contracts.EmptyBasePlugin
}

func (m *mockCredentialPluginContract[T]) GetConsumerIdentity(_ context.Context, _ v1.GetConsumerIdentityRequest[T]) (runtime.Identity, error) {
	return map[string]string{"id": "mock-identity"}, nil
}

func (m *mockCredentialPluginContract[T]) Resolve(_ context.Context, _ v1.ResolveRequest[T], _ runtime.Typed) (runtime.Typed, error) {
	return &runtime.Raw{Data: []byte(`{"resolved":"mock-credentials"}`)}, nil
}

var _ v1.CredentialPluginContract[*dummyv1.Repository] = &mockCredentialPluginContract[*dummyv1.Repository]{}

func TestRegisterCredentialPlugin(t *testing.T) {
	r := require.New(t)

	tests := []struct {
		name             string
		setupScheme      func(*runtime.Scheme)
		proto            *dummyv1.Repository
		plugin           v1.CredentialPluginContract[*dummyv1.Repository]
		expectError      bool
		expectedTypes    int
		expectedHandlers int
	}{
		{
			name: "successful registration",
			setupScheme: func(scheme *runtime.Scheme) {
				dummytype.MustAddToScheme(scheme)
			},
			proto:            &dummyv1.Repository{},
			plugin:           &mockCredentialPluginContract[*dummyv1.Repository]{},
			expectedTypes:    1,
			expectedHandlers: 2,
		},
		{
			name:        "invalid prototype",
			setupScheme: func(_ *runtime.Scheme) {},
			proto:       &dummyv1.Repository{},
			plugin:      &mockCredentialPluginContract[*dummyv1.Repository]{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			tt.setupScheme(scheme)
			builder := endpoints.NewEndpoints(scheme)

			err := RegisterCredentialPlugin(tt.proto, tt.plugin, builder)
			if tt.expectError {
				r.Error(err)
				return
			}
			r.NoError(err)

			rawPluginSpec, err := pluginruntime.ConvertToSpec(&builder.PluginSpec)
			r.NoError(err)
			content, err := json.Marshal(rawPluginSpec)
			r.NoError(err)
			if tt.expectedTypes > 0 {
				r.Contains(string(content), `"capabilities"`)
				r.Contains(string(content), `"credentialPlugin"`)
			}

			handlers := builder.GetHandlers()
			r.Len(handlers, tt.expectedHandlers)

			if tt.expectedHandlers > 0 {
				found := make(map[string]bool)
				for _, h := range handlers {
					found[h.Location] = true
				}
				r.True(found[GetConsumerIdentityEndpoint], "GetConsumerIdentity endpoint should be registered")
				r.True(found[ResolveEndpoint], "Resolve endpoint should be registered")
			}
		})
	}
}

func TestRegisterCredentialPlugin_MultipleTypes(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	builder := endpoints.NewEndpoints(scheme)

	r.NoError(RegisterCredentialPlugin(&dummyv1.Repository{}, &mockCredentialPluginContract[*dummyv1.Repository]{}, builder))
	r.NoError(RegisterCredentialPlugin(&dummyv1.Repository{BaseUrl: "different-url"}, &mockCredentialPluginContract[*dummyv1.Repository]{}, builder))

	r.Len(builder.GetHandlers(), 4)

	rawPluginSpec, err := pluginruntime.ConvertToSpec(&builder.PluginSpec)
	r.NoError(err)
	content, err := json.Marshal(rawPluginSpec)
	r.NoError(err)
	r.Contains(string(content), `"credentialPlugin"`)
}
