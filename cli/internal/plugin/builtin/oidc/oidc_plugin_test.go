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

func Test_OIDCPlugin_Resolve_TokenExchange(t *testing.T) {
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
	tests := []struct {
		name        string
		identity    runtime.Identity
		envKey      string
		envVal      string
		errContains string
	}{
		{
			name:        "missing tokenURL",
			identity:    runtime.Identity{configKeyFlow: flowTokenExchange},
			errContains: "tokenURL",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			if tt.envKey != "" {
				t.Setenv(tt.envKey, tt.envVal)
			}

			plugin := &OIDCPlugin{}
			_, err := plugin.Resolve(t.Context(), tt.identity, nil)
			r.Error(err)
			r.Contains(err.Error(), tt.errContains)
		})
	}
}

func Test_OIDCPlugin_Resolve_TokenExchange_Priority(t *testing.T) {
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
