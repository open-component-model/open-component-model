package sigstore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/oidcflow"
)

const (
	OIDCPluginType    = "SigstoreOIDC"
	OIDCPluginVersion = "v1alpha1"

	configKeyIssuer    = "issuer"
	configKeyClientID  = "clientID"
	credentialKeyToken = "token"
)

// OIDCPluginTypeVersioned is the fully qualified type for the SigstoreOIDC credential plugin.
var OIDCPluginTypeVersioned = runtime.NewVersionedType(OIDCPluginType, OIDCPluginVersion)

// OIDCPlugin implements credentials.CredentialPlugin for interactive OIDC
// token acquisition. It resolves tokens via the SIGSTORE_ID_TOKEN environment
// variable or an interactive browser-based OIDC flow.
//
// Example .ocmconfig entry:
//
//	consumers:
//	- identity:
//	    type: OIDCIdentityToken/v1alpha1
//	    algorithm: sigstore
//	    signature: mysig
//	  credentials:
//	  - type: SigstoreOIDC/v1alpha1
//	    issuer: https://oauth2.sigstore.dev/auth
//	    clientID: sigstore
type OIDCPlugin struct{}

var _ credentials.CredentialPlugin = (*OIDCPlugin)(nil)

// GetConsumerIdentity maps a SigstoreOIDC credential to a consumer identity.
func (p *OIDCPlugin) GetConsumerIdentity(_ context.Context, credential runtime.Typed) (runtime.Identity, error) {
	cfg, err := parseOIDCConfig(credential)
	if err != nil {
		return nil, err
	}
	id := runtime.Identity{
		configKeyIssuer:   cfg.issuer,
		configKeyClientID: cfg.clientID,
	}
	id.SetType(OIDCPluginTypeVersioned)
	return id, nil
}

// Resolve acquires an OIDC identity token. It checks SIGSTORE_ID_TOKEN first,
// then falls back to an interactive browser-based OIDC flow.
func (p *OIDCPlugin) Resolve(ctx context.Context, identity runtime.Identity, _ map[string]string) (map[string]string, error) {
	if tok := os.Getenv("SIGSTORE_ID_TOKEN"); tok != "" {
		return map[string]string{credentialKeyToken: tok}, nil
	}

	issuer := identity[configKeyIssuer]
	clientID := identity[configKeyClientID]

	token, err := oidcflow.GetIDToken(ctx, oidcflow.Options{
		Issuer:   issuer,
		ClientID: clientID,
	})
	if err != nil {
		return nil, fmt.Errorf("interactive OIDC authentication: %w", err)
	}

	return map[string]string{credentialKeyToken: token.RawToken}, nil
}

type oidcConfig struct {
	issuer   string
	clientID string
}

func parseOIDCConfig(typed runtime.Typed) (*oidcConfig, error) {
	var raw struct {
		Issuer   string `json:"issuer"`
		ClientID string `json:"clientID"`
	}

	data, err := json.Marshal(typed)
	if err != nil {
		return nil, fmt.Errorf("marshal credential: %w", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal credential: %w", err)
	}

	cfg := &oidcConfig{
		issuer:   oidcflow.DefaultIssuer,
		clientID: oidcflow.DefaultClientID,
	}
	if raw.Issuer != "" {
		cfg.issuer = raw.Issuer
	}
	if raw.ClientID != "" {
		cfg.clientID = raw.ClientID
	}
	return cfg, nil
}
