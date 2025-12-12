package credentialrepository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/credentials/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockCredentialPlugin[T runtime.Typed] struct {
	contracts.EmptyBasePlugin
}

func (m *mockCredentialPlugin[T]) ConsumerIdentityForConfig(ctx context.Context, cfg v1.ConsumerIdentityForConfigRequest[T]) (runtime.Identity, error) {
	return map[string]string{"id": "mock-identity"}, nil
}

func (m *mockCredentialPlugin[T]) Resolve(ctx context.Context, cfg v1.ResolveRequest[T], credentials map[string]string) (map[string]string, error) {
	return map[string]string{"resolved": "mock-credentials"}, nil
}

var _ v1.CredentialRepositoryPluginContract[*dummyv1.Repository] = &mockCredentialPlugin[*dummyv1.Repository]{}

func TestRegisterCredentialRepository(t *testing.T) {
	r := require.New(t)

	tests := []struct {
		name             string
		setupScheme      func(*runtime.Scheme)
		proto            *dummyv1.Repository
		plugin           v1.CredentialRepositoryPluginContract[*dummyv1.Repository]
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
			plugin:           &mockCredentialPlugin[*dummyv1.Repository]{},
			expectError:      false,
			expectedTypes:    1,
			expectedHandlers: 2,
		},
		{
			name:             "invalid prototype",
			setupScheme:      func(scheme *runtime.Scheme) {},
			proto:            &dummyv1.Repository{},
			plugin:           &mockCredentialPlugin[*dummyv1.Repository]{},
			expectError:      true,
			expectedTypes:    0,
			expectedHandlers: 0,
		},
		{
			name: "duplicate handler registration",
			setupScheme: func(scheme *runtime.Scheme) {
				dummytype.MustAddToScheme(scheme)
			},
			proto:            &dummyv1.Repository{},
			plugin:           &mockCredentialPlugin[*dummyv1.Repository]{},
			expectError:      false,
			expectedTypes:    1,
			expectedHandlers: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			tt.setupScheme(scheme)
			builder := endpoints.NewEndpoints(scheme)

			// Register the credential repository
			err := RegisterCredentialRepository(tt.proto, tt.plugin, builder)
			if tt.expectError {
				r.Error(err)
				return
			}
			r.NoError(err)

			// Validate registered types
			rawPluginSpec, err := pluginruntime.ConvertToSpec(&builder.PluginSpec)
			r.NoError(err)
			// Validate registered types
			content, err := json.Marshal(rawPluginSpec)
			r.NoError(err)
			if tt.expectedTypes > 0 {
				r.Contains(string(content), `"capabilities"`)
			} else {
				r.NotContains(string(content), `"capabilities"`)
			}

			// Validate handler count
			handlers := builder.GetHandlers()
			r.Len(handlers, tt.expectedHandlers)

			// Validate specific endpoints are registered
			if tt.expectedHandlers > 0 {
				found := make(map[string]bool)
				for _, handler := range handlers {
					found[handler.Location] = true
				}
				r.True(found[ConsumerIdentityForConfig], "ConsumerIdentityForConfig endpoint should be registered")
				r.True(found[Resolve], "Resolve endpoint should be registered")
			}
		})
	}
}

func TestRegisterCredentialRepository_MultipleTypes(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme()
	dummytype.MustAddToScheme(scheme)
	builder := endpoints.NewEndpoints(scheme)

	// Register first type
	err := RegisterCredentialRepository(&dummyv1.Repository{}, &mockCredentialPlugin[*dummyv1.Repository]{}, builder)
	r.NoError(err)

	// Register second type (same plugin type, different instance)
	secondRepo := &dummyv1.Repository{BaseUrl: "different-url"}
	err = RegisterCredentialRepository(secondRepo, &mockCredentialPlugin[*dummyv1.Repository]{}, builder)
	r.NoError(err)

	// Should have 4 handlers total (2 for each registration)
	handlers := builder.GetHandlers()
	r.Len(handlers, 4)

	// Should have 2 types registered
	// Validate registered types
	rawPluginSpec, err := pluginruntime.ConvertToSpec(&builder.PluginSpec)
	r.NoError(err)
	// Validate registered types
	content, err := json.Marshal(rawPluginSpec)
	r.NoError(err)
	r.Contains(string(content), `"credentialRepository"`)
}
