package oidc

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_OIDCPlugin_GetConsumerIdentity(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	plugin := &OIDCPlugin{}

	raw := &runtime.Raw{}
	raw.SetType(OIDCPluginTypeVersioned)
	raw.Data = []byte(`{"type":"OIDCProvider/v1alpha1","issuer":"https://custom.issuer.dev","clientID":"my-client"}`)

	id, err := plugin.GetConsumerIdentity(t.Context(), raw)
	r.NoError(err)
	r.Equal("https://custom.issuer.dev", id[configKeyIssuer])
	r.Equal("my-client", id[configKeyClientID])

	idType, err := id.ParseType()
	r.NoError(err)
	r.Equal(OIDCPluginTypeVersioned, idType)
}

func Test_OIDCPlugin_GetConsumerIdentity_Defaults(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	plugin := &OIDCPlugin{}

	raw := &runtime.Raw{}
	raw.SetType(OIDCPluginTypeVersioned)
	raw.Data = []byte(`{"type":"OIDCProvider/v1alpha1"}`)

	id, err := plugin.GetConsumerIdentity(t.Context(), raw)
	r.NoError(err)
	r.Equal("https://oauth2.sigstore.dev/auth", id[configKeyIssuer])
	r.Equal("sigstore", id[configKeyClientID])
}
