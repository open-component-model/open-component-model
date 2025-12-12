package digestprocessor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts/digestprocessor/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockPlugin struct {
	contracts.EmptyBasePlugin
}

func (m *mockPlugin) GetIdentity(ctx context.Context, typ *v1.GetIdentityRequest[runtime.Typed]) (*v1.GetIdentityResponse, error) {
	//TODO implement me
	panic("implement me")
}

func (m *mockPlugin) ProcessResourceDigest(ctx context.Context, req *v1.ProcessResourceDigestRequest, credentials map[string]string) (*v1.ProcessResourceDigestResponse, error) {
	//TODO implement me
	panic("implement me")
}

var _ v1.ResourceDigestProcessorContract = &mockPlugin{}

func TestRegisterInputProcessor(t *testing.T) {
	r := require.New(t)

	tests := []struct {
		name             string
		setupScheme      func(*runtime.Scheme)
		proto            runtime.Typed
		plugin           v1.ResourceDigestProcessorContract
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
			plugin:           &mockPlugin{},
			expectError:      false,
			expectedTypes:    1,
			expectedHandlers: 2,
		},
		{
			name:             "invalid prototype",
			setupScheme:      func(scheme *runtime.Scheme) {},
			proto:            &dummyv1.Repository{},
			plugin:           &mockPlugin{},
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
			plugin:           &mockPlugin{},
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

			// Register the processor
			err := RegisterDigestProcessor(tt.proto, tt.plugin, builder)
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
		})
	}
}
