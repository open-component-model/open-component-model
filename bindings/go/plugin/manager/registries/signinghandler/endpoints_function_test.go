package signinghandler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/plugin/internal/dummytype"
	dummyv1 "ocm.software/open-component-model/bindings/go/plugin/internal/dummytype/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/contracts"
	v1 "ocm.software/open-component-model/bindings/go/plugin/manager/contracts/signing/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/endpoints"
	pluginruntime "ocm.software/open-component-model/bindings/go/plugin/manager/types/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type mockSigningPlugin struct{ contracts.EmptyBasePlugin }

func (m *mockSigningPlugin) GetSignerIdentity(ctx context.Context, req *v1.GetSignerIdentityRequest[runtime.Typed]) (*v1.IdentityResponse, error) {
	// not called in registration tests
	panic("not implemented")
}

func (m *mockSigningPlugin) Sign(ctx context.Context, request *v1.SignRequest[runtime.Typed], credentials map[string]string) (*v1.SignResponse, error) {
	// not called in registration tests
	panic("not implemented")
}

func (m *mockSigningPlugin) GetVerifierIdentity(ctx context.Context, req *v1.GetVerifierIdentityRequest[runtime.Typed]) (*v1.IdentityResponse, error) {
	// not called in registration tests
	panic("not implemented")
}

func (m *mockSigningPlugin) Verify(ctx context.Context, request *v1.VerifyRequest[runtime.Typed], credentials map[string]string) (*v1.VerifyResponse, error) {
	// not called in registration tests
	panic("not implemented")
}

var _ v1.SignatureHandlerContract[runtime.Typed] = &mockSigningPlugin{}

func TestRegisterSigningHandler(t *testing.T) {
	r := require.New(t)

	tests := []struct {
		name             string
		setupScheme      func(*runtime.Scheme)
		proto            runtime.Typed
		plugin           v1.SignatureHandlerContract[runtime.Typed]
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
			plugin:           &mockSigningPlugin{},
			expectError:      false,
			expectedTypes:    1,
			expectedHandlers: 4,
		},
		{
			name:             "invalid prototype",
			setupScheme:      func(scheme *runtime.Scheme) {},
			proto:            &dummyv1.Repository{},
			plugin:           &mockSigningPlugin{},
			expectError:      true,
			expectedTypes:    0,
			expectedHandlers: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			tt.setupScheme(scheme)
			builder := endpoints.NewEndpoints(scheme)

			err := RegisterPlugin(tt.proto, tt.plugin, builder)
			if tt.expectError {
				r.Error(err)
				return
			}
			r.NoError(err)

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

			// Additionally ensure exactly one plugin type bucket is present
			r.NoError(json.Unmarshal(content, rawPluginSpec))
			pluginSpec, err := pluginruntime.ConvertFromSpec(v1.Scheme, rawPluginSpec)
			r.NoError(err)
			r.Equal(tt.expectedTypes, len(pluginSpec.CapabilitySpecs))
		})
	}
}
