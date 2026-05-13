package oidc

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_OIDCPlugin_GetCredentialPluginScheme(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	plugin := &OIDCPlugin{}
	scheme := plugin.GetCredentialPluginScheme()
	r.NotNil(scheme)

	types := scheme.GetTypes()
	r.NotEmpty(types)

	unversioned := runtime.NewUnversionedType(OIDCPluginType)
	aliases, hasDefault := types[unversioned]
	r.True(hasDefault, "scheme must register unversioned type %s as default", unversioned)
	r.Contains(aliases, OIDCPluginTypeVersioned, "scheme must register versioned type as alias")
}

func Test_OIDCPlugin_GetConsumerIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "with custom values",
			config: `{"type":"OIDCIdentityTokenProvider/v1alpha1","issuer":"https://custom.issuer.dev","clientID":"my-client"}`,
		},
		{
			name:   "minimal",
			config: `{"type":"OIDCIdentityTokenProvider/v1alpha1"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			plugin := &OIDCPlugin{}
			raw := &runtime.Raw{}
			raw.SetType(OIDCPluginTypeVersioned)
			raw.Data = []byte(tt.config)

			id, err := plugin.GetConsumerIdentity(t.Context(), raw)
			r.NoError(err)

			idType, err := id.ParseType()
			r.NoError(err)
			r.Equal(OIDCPluginTypeVersioned, idType)
		})
	}
}

func Test_OIDCPlugin_GetConsumerIdentity_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		credential  runtime.Typed
		errContains string
	}{
		{
			name:        "nil credential",
			credential:  nil,
			errContains: "must not be nil",
		},
		{
			name:        "empty type",
			credential:  &runtime.Raw{},
			errContains: "must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			plugin := &OIDCPlugin{}
			_, err := plugin.GetConsumerIdentity(t.Context(), tt.credential)
			r.Error(err)
			r.Contains(err.Error(), tt.errContains)
		})
	}
}
