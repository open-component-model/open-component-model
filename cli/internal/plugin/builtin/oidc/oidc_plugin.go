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
//
// TODO(cred-graph-vs-cred-plugin): the credential-graph model (ADR 0002, ADR 0021)
// assumes plugins like HashiCorpVault put disambiguating fields (address, hostname,
// auth-type) on the credential body, so each consumer that references the plugin
// gets a distinct leaf vertex. The OIDC plugin has no such fields on the credential
// body — issuer and clientID live on the consumer identity — so all consumers
// referencing this plugin collapse onto a single type-only leaf. This is currently
// safe only because the consumer identity (not the
// leaf identity) is routed into Resolve (see PR #2511). Revisit if that contract changes, or if a future
// .ocmconfig syntax allows OIDC parameters on the credential entry itself.
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
//
// TODO(cred-graph-vs-cred-plugin): this plugin reads its parameters from the consumer
// identity, not from a resolved credential map on the leaf. This depends on
// credential graph PR #2511, which passes the consumer identity (not the leaf credential
// identity) into plugin.Resolve. See GetConsumerIdentity above for the exposed design issue.
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
