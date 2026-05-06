package oidc

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_OIDCPlugin_GetConsumerIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   string
		assertFn func(t *testing.T, id runtime.Identity)
	}{
		{
			name:   "interactive with custom values",
			config: `{"type":"OIDCIdentityTokenProvider/v1alpha1","issuer":"https://custom.issuer.dev","clientID":"my-client"}`,
			assertFn: func(t *testing.T, id runtime.Identity) {
				r := require.New(t)
				r.Equal("https://custom.issuer.dev", id[configKeyIssuer])
				r.Equal("my-client", id[configKeyClientID])
				r.Empty(id[configKeyFlow])
			},
		},
		{
			name:   "interactive with defaults",
			config: `{"type":"OIDCIdentityTokenProvider/v1alpha1"}`,
			assertFn: func(t *testing.T, id runtime.Identity) {
				r := require.New(t)
				r.Equal("https://oauth2.sigstore.dev/auth", id[configKeyIssuer])
				r.Equal("sigstore", id[configKeyClientID])
			},
		},
		{
			name:   "authorization-code explicit flow",
			config: `{"type":"OIDCIdentityTokenProvider/v1alpha1","flow":"authorization-code","issuer":"https://accounts.google.com","clientID":"my-app"}`,
			assertFn: func(t *testing.T, id runtime.Identity) {
				r := require.New(t)
				r.Equal("https://accounts.google.com", id[configKeyIssuer])
				r.Equal("my-app", id[configKeyClientID])
				r.Empty(id[configKeyFlow])
			},
		},
		{
			name:   "token-exchange with issuer",
			config: `{"type":"OIDCIdentityTokenProvider/v1alpha1","flow":"token-exchange","issuer":"https://keycloak.example.com/realms/myrealm","subjectTokenEnvVar":"K8S_TOKEN"}`,
			assertFn: func(t *testing.T, id runtime.Identity) {
				r := require.New(t)
				r.Equal("token-exchange", id[configKeyFlow])
				r.Equal("https://keycloak.example.com/realms/myrealm", id[configKeyIssuer])
				r.Empty(id[configKeyTokenURL])
				r.Equal("K8S_TOKEN", id[configKeySubjectTokenEnvVar])
			},
		},
		{
			name:   "token-exchange",
			config: `{"type":"OIDCIdentityTokenProvider/v1alpha1","flow":"token-exchange","tokenURL":"https://sts.example.com/token","subjectTokenEnvVar":"CI_TOKEN"}`,
			assertFn: func(t *testing.T, id runtime.Identity) {
				r := require.New(t)
				r.Equal("token-exchange", id[configKeyFlow])
				r.Equal("https://sts.example.com/token", id[configKeyTokenURL])
				r.Equal("CI_TOKEN", id[configKeySubjectTokenEnvVar])
				r.Empty(id[configKeyIssuer])
				r.Empty(id[configKeyClientID])
			},
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

			tt.assertFn(t, id)
		})
	}
}

func Test_OIDCPlugin_GetConsumerIdentity_UnknownFlow(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	plugin := &OIDCPlugin{}
	raw := &runtime.Raw{}
	raw.SetType(OIDCPluginTypeVersioned)
	raw.Data = []byte(`{"type":"OIDCIdentityTokenProvider/v1alpha1","flow":"invalid-flow"}`)

	_, err := plugin.GetConsumerIdentity(t.Context(), raw)
	r.Error(err)
	r.Contains(err.Error(), "unknown flow")
}

func Test_OIDCPlugin_Resolve_TokenExchange(t *testing.T) {
	// not parallel: uses t.Setenv
	r := require.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.Equal(http.MethodPost, req.Method)
		r.NoError(req.ParseForm())
		r.Equal("urn:ietf:params:oauth:grant-type:token-exchange", req.PostForm.Get("grant_type"))
		r.Equal("my-ci-token-value", req.PostForm.Get("subject_token"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "exchanged-id-token"}) //nolint:errcheck
	}))
	defer srv.Close()

	t.Setenv("TEST_OIDC_SUBJECT_TOKEN", "my-ci-token-value")

	plugin := &OIDCPlugin{}
	identity := runtime.Identity{
		configKeyFlow:               flowTokenExchange,
		configKeyTokenURL:           srv.URL,
		configKeySubjectTokenEnvVar: "TEST_OIDC_SUBJECT_TOKEN",
	}

	creds, err := plugin.Resolve(t.Context(), identity, nil)
	r.NoError(err)
	r.Equal("exchanged-id-token", creds[credentialKeyToken])
}

func Test_OIDCPlugin_Resolve_TokenExchange_Errors(t *testing.T) {
	// not parallel: subtests use t.Setenv
	tests := []struct {
		name        string
		identity    runtime.Identity
		envKey      string
		envVal      string
		setupFile   func(t *testing.T) string
		errContains string
	}{
		{
			name:        "missing tokenURL",
			identity:    runtime.Identity{configKeyFlow: flowTokenExchange},
			errContains: "issuer or tokenURL",
		},
		{
			name: "no token source",
			identity: runtime.Identity{
				configKeyFlow:     flowTokenExchange,
				configKeyTokenURL: "https://sts.example.com/token",
			},
			errContains: "no subject token",
		},
		{
			name: "env var empty",
			identity: runtime.Identity{
				configKeyFlow:               flowTokenExchange,
				configKeyTokenURL:           "https://sts.example.com/token",
				configKeySubjectTokenEnvVar: "TEST_OIDC_ERR_TOKEN",
				configKeySubjectToken:       "fallback-literal",
			},
			envKey:      "TEST_OIDC_ERR_TOKEN",
			envVal:      "",
			errContains: "empty or unset",
		},
		{
			name: "token file empty",
			identity: runtime.Identity{
				configKeyFlow:     flowTokenExchange,
				configKeyTokenURL: "https://sts.example.com/token",
			},
			setupFile: func(t *testing.T) string {
				f := filepath.Join(t.TempDir(), "empty-token")
				require.NoError(t, os.WriteFile(f, []byte("  \n"), 0o600))
				return f
			},
			errContains: "is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envVal)
			}
			identity := tt.identity
			if tt.setupFile != nil {
				identity[configKeySubjectTokenFile] = tt.setupFile(t)
			}

			plugin := &OIDCPlugin{}
			_, err := plugin.Resolve(t.Context(), identity, nil)
			r.Error(err)
			r.Contains(err.Error(), tt.errContains)
		})
	}
}

func Test_OIDCPlugin_Resolve_TokenExchange_Priority(t *testing.T) {
	// not parallel: uses t.Setenv
	r := require.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.NoError(req.ParseForm())
		r.Equal("env-token-value", req.PostForm.Get("subject_token"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "result-token"}) //nolint:errcheck
	}))
	defer srv.Close()

	t.Setenv("TEST_OIDC_PRIORITY_TOKEN", "env-token-value")

	plugin := &OIDCPlugin{}
	identity := runtime.Identity{
		configKeyFlow:               flowTokenExchange,
		configKeyTokenURL:           srv.URL,
		configKeySubjectTokenEnvVar: "TEST_OIDC_PRIORITY_TOKEN",
		configKeySubjectToken:       "literal-token-value",
	}

	creds, err := plugin.Resolve(t.Context(), identity, nil)
	r.NoError(err)
	r.Equal("result-token", creds[credentialKeyToken])
}

func Test_OIDCPlugin_Resolve_TokenExchange_FileSource(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.NoError(req.ParseForm())
		r.Equal("file-token-value", req.PostForm.Get("subject_token"))

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "file-result-token"}) //nolint:errcheck
	}))
	defer srv.Close()

	tokenFile := filepath.Join(t.TempDir(), "token")
	r.NoError(os.WriteFile(tokenFile, []byte("file-token-value\n"), 0o600))

	plugin := &OIDCPlugin{}
	identity := runtime.Identity{
		configKeyFlow:             flowTokenExchange,
		configKeyTokenURL:         srv.URL,
		configKeySubjectTokenFile: tokenFile,
	}

	creds, err := plugin.Resolve(t.Context(), identity, nil)
	r.NoError(err)
	r.Equal("file-result-token", creds[credentialKeyToken])
}

func Test_OIDCPlugin_Resolve_TokenExchange_IssuerDiscovery(t *testing.T) {
	// not parallel: uses t.Setenv
	r := require.New(t)

	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	defer srv.Close()

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"issuer":                 srv.URL,
			"token_endpoint":         srv.URL + "/token",
			"authorization_endpoint": srv.URL + "/authorize",
			"jwks_uri":               srv.URL + "/keys",
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, req *http.Request) {
		r.NoError(req.ParseForm())
		r.Equal("my-subject-token", req.PostForm.Get("subject_token"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"access_token": "discovered-token"}) //nolint:errcheck
	})

	t.Setenv("TEST_DISCOVERY_TOKEN", "my-subject-token")

	plugin := &OIDCPlugin{}
	identity := runtime.Identity{
		configKeyFlow:               flowTokenExchange,
		configKeyIssuer:             srv.URL,
		configKeySubjectTokenEnvVar: "TEST_DISCOVERY_TOKEN",
	}

	creds, err := plugin.Resolve(t.Context(), identity, nil)
	r.NoError(err)
	r.Equal("discovered-token", creds[credentialKeyToken])
}
