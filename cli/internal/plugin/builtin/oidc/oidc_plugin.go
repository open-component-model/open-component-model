package oidc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"ocm.software/open-component-model/bindings/go/credentials"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/cli/internal/oidcflow"
)

const (
	OIDCPluginType    = "OIDCIdentityTokenProvider"
	OIDCPluginVersion = "v1alpha1"

	configKeyIssuer             = "issuer"
	configKeyClientID           = "clientID"
	configKeyFlow               = "flow"
	configKeyTokenURL           = "tokenURL"
	configKeySubjectToken       = "subjectToken"
	configKeySubjectTokenEnvVar = "subjectTokenEnvVar"
	configKeySubjectTokenFile   = "subjectTokenFile"
	configKeySubjectTokenType   = "subjectTokenType"
	configKeyAudience           = "audience"
	credentialKeyToken          = "token"

	flowInteractive   = "interactive"
	flowTokenExchange = "token-exchange"
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

// OIDCPlugin implements credentials.CredentialPlugin for interactive OIDC
// token acquisition via a browser-based OIDC flow.
//
// Example .ocmconfig entry:
//
//	consumers:
//	- identity:
//	    type: SigstoreSigner/v1alpha1
//	    algorithm: sigstore
//	    signature: mysig
//	  credentials:
//	  - type: OIDCIdentityTokenProvider/v1alpha1
type OIDCPlugin struct{}

var _ credentials.CredentialPlugin = (*OIDCPlugin)(nil)

func (p *OIDCPlugin) GetCredentialPluginScheme() *runtime.Scheme {
	return pluginScheme
}

// GetConsumerIdentity maps an OIDCIdentityTokenProvider credential to a consumer identity.
func (p *OIDCPlugin) GetConsumerIdentity(_ context.Context, credential runtime.Typed) (runtime.Identity, error) {
	cfg, err := parseOIDCConfig(credential)
	if err != nil {
		return nil, err
	}

	id := runtime.Identity{}
	if cfg.flow == flowTokenExchange {
		id[configKeyFlow] = cfg.flow
		id[configKeyTokenURL] = cfg.tokenURL
		if cfg.subjectTokenEnvVar != "" {
			id[configKeySubjectTokenEnvVar] = cfg.subjectTokenEnvVar
		}
		if cfg.subjectToken != "" {
			id[configKeySubjectToken] = cfg.subjectToken
		}
		if cfg.subjectTokenFile != "" {
			id[configKeySubjectTokenFile] = cfg.subjectTokenFile
		}
	} else {
		id[configKeyIssuer] = cfg.issuer
		id[configKeyClientID] = cfg.clientID
	}
	id.SetType(OIDCPluginTypeVersioned)
	return id, nil
}

// Resolve acquires an OIDC identity token via the configured flow.
func (p *OIDCPlugin) Resolve(ctx context.Context, identity runtime.Identity, _ map[string]string) (map[string]string, error) {
	flow := identity[configKeyFlow]
	if flow == flowTokenExchange {
		return p.resolveTokenExchange(ctx, identity)
	}
	return p.resolveInteractive(ctx, identity)
}

func (p *OIDCPlugin) resolveInteractive(ctx context.Context, identity runtime.Identity) (map[string]string, error) {
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

func (p *OIDCPlugin) resolveTokenExchange(ctx context.Context, identity runtime.Identity) (map[string]string, error) {
	tokenURL := identity[configKeyTokenURL]
	if tokenURL == "" {
		return nil, fmt.Errorf("token-exchange flow requires tokenURL")
	}

	subjectToken, err := resolveSubjectToken(identity)
	if err != nil {
		return nil, err
	}

	token, err := oidcflow.ExchangeToken(ctx, oidcflow.ExchangeOptions{
		TokenURL:         tokenURL,
		SubjectToken:     subjectToken,
		SubjectTokenType: identity[configKeySubjectTokenType],
		Audience:         identity[configKeyAudience],
	})
	if err != nil {
		return nil, fmt.Errorf("token-exchange authentication: %w", err)
	}

	return map[string]string{credentialKeyToken: token.RawToken}, nil
}

func resolveSubjectToken(identity runtime.Identity) (string, error) {
	if envVar := identity[configKeySubjectTokenEnvVar]; envVar != "" {
		if val := os.Getenv(envVar); val != "" {
			return val, nil
		}
	}
	if literal := identity[configKeySubjectToken]; literal != "" {
		return literal, nil
	}
	if file := identity[configKeySubjectTokenFile]; file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("read subject token file: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
	return "", fmt.Errorf("no subject token resolvable: set subjectTokenEnvVar, subjectToken, or subjectTokenFile")
}

type oidcConfig struct {
	issuer             string
	clientID           string
	flow               string
	tokenURL           string
	subjectToken       string
	subjectTokenEnvVar string
	subjectTokenFile   string
	subjectTokenType   string
	audience           string
}

func parseOIDCConfig(typed runtime.Typed) (*oidcConfig, error) {
	var raw struct {
		Issuer             string `json:"issuer"`
		ClientID           string `json:"clientID"`
		Flow               string `json:"flow"`
		TokenURL           string `json:"tokenURL"`
		SubjectToken       string `json:"subjectToken"`
		SubjectTokenEnvVar string `json:"subjectTokenEnvVar"`
		SubjectTokenFile   string `json:"subjectTokenFile"`
		SubjectTokenType   string `json:"subjectTokenType"`
		Audience           string `json:"audience"`
	}

	data, err := json.Marshal(typed)
	if err != nil {
		return nil, fmt.Errorf("marshal credential: %w", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal credential: %w", err)
	}

	cfg := &oidcConfig{
		issuer:           oidcflow.DefaultIssuer,
		clientID:         oidcflow.DefaultClientID,
		flow:             flowInteractive,
		subjectTokenType: oidcflow.DefaultSubjectTokenType,
		audience:         oidcflow.DefaultAudience,
	}
	if raw.Issuer != "" {
		cfg.issuer = raw.Issuer
	}
	if raw.ClientID != "" {
		cfg.clientID = raw.ClientID
	}
	if raw.Flow != "" {
		cfg.flow = raw.Flow
	}
	if raw.TokenURL != "" {
		cfg.tokenURL = raw.TokenURL
	}
	if raw.SubjectToken != "" {
		cfg.subjectToken = raw.SubjectToken
	}
	if raw.SubjectTokenEnvVar != "" {
		cfg.subjectTokenEnvVar = raw.SubjectTokenEnvVar
	}
	if raw.SubjectTokenFile != "" {
		cfg.subjectTokenFile = raw.SubjectTokenFile
	}
	if raw.SubjectTokenType != "" {
		cfg.subjectTokenType = raw.SubjectTokenType
	}
	if raw.Audience != "" {
		cfg.audience = raw.Audience
	}
	return cfg, nil
}
