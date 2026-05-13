package oidc

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/oidcflow"
)

const (
	OIDCPluginType    = "OIDCIdentityTokenProvider"
	OIDCPluginVersion = "v1alpha1"

	configKeyIssuer    = "issuer"
	configKeyClientID  = "clientID"
	credentialKeyToken = "token"
)

// OIDCPluginTypeVersioned is the fully qualified type for the OIDCIdentityTokenProvider credential plugin.
var OIDCPluginTypeVersioned = runtime.NewVersionedType(OIDCPluginType, OIDCPluginVersion)

var pluginScheme = runtime.NewScheme()

func init() {
	pluginScheme.MustRegisterWithAlias(&runtime.Raw{},
		runtime.NewUnversionedType(OIDCPluginType),
		OIDCPluginTypeVersioned,
	)
}

// OIDCPlugin implements credentials.CredentialPlugin for OIDC identity token
// acquisition via interactive browser-based authorization code flow with PKCE.
//
// Example .ocmconfig entry:
//
//	consumers:
//	- identity:
//	    type: SigstoreSigner/v1alpha1
//	    issuer: https://oauth2.sigstore.dev/auth
//	    clientID: sigstore
//	    signature: mysig
//	  credentials:
//	  - type: OIDCIdentityTokenProvider/v1alpha1
type OIDCPlugin struct{}

var _ credentials.CredentialPlugin = (*OIDCPlugin)(nil)

func (p *OIDCPlugin) GetCredentialPluginScheme() *runtime.Scheme {
	return pluginScheme
}

// GetConsumerIdentity returns the credential identity used for graph node matching.
func (p *OIDCPlugin) GetConsumerIdentity(_ context.Context, credential runtime.Typed) (runtime.Identity, error) {
	if credential == nil {
		return nil, fmt.Errorf("credential must not be nil")
	}
	if credential.GetType().IsEmpty() {
		return nil, fmt.Errorf("credential type must not be empty")
	}
	id := runtime.Identity{}
	id.SetType(OIDCPluginTypeVersioned)
	return id, nil
}

// Resolve acquires an OIDC identity token via interactive authorization code flow.
// issuer and clientID are read from the consumer identity. If empty, defaults to Sigstore public.
func (p *OIDCPlugin) Resolve(ctx context.Context, identity runtime.Identity, _ map[string]string) (map[string]string, error) {
	token, err := oidcflow.GetIDToken(ctx, oidcflow.Options{
		Issuer:   identity[configKeyIssuer],
		ClientID: identity[configKeyClientID],
	})
	if err != nil {
		return nil, fmt.Errorf("OIDC authentication: %w", err)
	}
	return map[string]string{credentialKeyToken: token.RawToken}, nil
}
