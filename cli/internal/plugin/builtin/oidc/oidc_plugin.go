package oidc

import (
	"context"
	"cmp"
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

	flowTokenExchange    = "token-exchange"
	flowAuthorizationCode = "authorization-code"
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
// acquisition, supporting both interactive browser-based flows and non-interactive
// RFC 8693 token exchange for CI/CD environments.
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
		if cfg.tokenURL != "" {
			id[configKeyTokenURL] = cfg.tokenURL
		}
		if cfg.issuer != "" {
			id[configKeyIssuer] = cfg.issuer
		}
		id[configKeySubjectTokenType] = cfg.subjectTokenType
		id[configKeyAudience] = cfg.audience
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
		issuer := identity[configKeyIssuer]
		if issuer == "" {
			return nil, fmt.Errorf("token-exchange flow requires issuer or tokenURL")
		}
		discovered, err := oidcflow.DiscoverTokenURL(ctx, issuer, nil)
		if err != nil {
			return nil, err
		}
		tokenURL = discovered
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
		val := os.Getenv(envVar)
		if val != "" {
			return val, nil
		}
		return "", fmt.Errorf("subject token env var %q is configured but empty or unset", envVar)
	}
	if literal := identity[configKeySubjectToken]; literal != "" {
		return literal, nil
	}
	if file := identity[configKeySubjectTokenFile]; file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("unable to read subject token file %q: %w", file, err)
		}
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", fmt.Errorf("subject token file %q is empty", file)
		}
		return token, nil
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
		flow:               raw.Flow,
		tokenURL:           raw.TokenURL,
		subjectToken:       raw.SubjectToken,
		subjectTokenEnvVar: raw.SubjectTokenEnvVar,
		subjectTokenFile:   raw.SubjectTokenFile,
		subjectTokenType:   cmp.Or(raw.SubjectTokenType, oidcflow.DefaultSubjectTokenType),
		audience:           cmp.Or(raw.Audience, oidcflow.DefaultAudience),
	}

	if raw.Flow == flowTokenExchange {
		cfg.issuer = raw.Issuer
		cfg.clientID = raw.ClientID
	} else {
		cfg.issuer = cmp.Or(raw.Issuer, oidcflow.DefaultIssuer)
		cfg.clientID = cmp.Or(raw.ClientID, oidcflow.DefaultClientID)
	}

	return cfg, nil
}
