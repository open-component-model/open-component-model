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
	r := require.New(t)

	plugin := &OIDCPlugin{}

	raw := &runtime.Raw{}
	raw.SetType(OIDCPluginTypeVersioned)
	raw.Data = []byte(`{"type":"OIDCIdentityTokenProvider/v1alpha1","issuer":"https://custom.issuer.dev","clientID":"my-client"}`)

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
	raw.Data = []byte(`{"type":"OIDCIdentityTokenProvider/v1alpha1"}`)

	id, err := plugin.GetConsumerIdentity(t.Context(), raw)
	r.NoError(err)
	r.Equal("https://oauth2.sigstore.dev/auth", id[configKeyIssuer])
	r.Equal("sigstore", id[configKeyClientID])
}

func Test_OIDCPlugin_GetConsumerIdentity_TokenExchange(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	plugin := &OIDCPlugin{}

	raw := &runtime.Raw{}
	raw.SetType(OIDCPluginTypeVersioned)
	raw.Data = []byte(`{"type":"OIDCIdentityTokenProvider/v1alpha1","flow":"token-exchange","tokenURL":"https://sts.example.com/token","subjectTokenEnvVar":"CI_TOKEN"}`)

	id, err := plugin.GetConsumerIdentity(t.Context(), raw)
	r.NoError(err)
	r.Equal("token-exchange", id[configKeyFlow])
	r.Equal("https://sts.example.com/token", id[configKeyTokenURL])
	r.Equal("CI_TOKEN", id[configKeySubjectTokenEnvVar])
	r.Empty(id[configKeyIssuer])
	r.Empty(id[configKeyClientID])

	idType, err := id.ParseType()
	r.NoError(err)
	r.Equal(OIDCPluginTypeVersioned, idType)
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
		configKeyFlow:              flowTokenExchange,
		configKeyTokenURL:          srv.URL,
		configKeySubjectTokenEnvVar: "TEST_OIDC_SUBJECT_TOKEN",
		configKeySubjectTokenType:  "urn:ietf:params:oauth:token-type:jwt",
		configKeyAudience:          "sigstore",
	}

	creds, err := plugin.Resolve(t.Context(), identity, nil)
	r.NoError(err)
	r.Equal("exchanged-id-token", creds[credentialKeyToken])
}

func Test_OIDCPlugin_Resolve_TokenExchange_MissingTokenURL(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	plugin := &OIDCPlugin{}
	identity := runtime.Identity{
		configKeyFlow: flowTokenExchange,
	}

	_, err := plugin.Resolve(t.Context(), identity, nil)
	r.Error(err)
	r.Contains(err.Error(), "tokenURL")
}

func Test_OIDCPlugin_Resolve_TokenExchange_NoTokenSource(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	plugin := &OIDCPlugin{}
	identity := runtime.Identity{
		configKeyFlow:     flowTokenExchange,
		configKeyTokenURL: "https://sts.example.com/token",
	}

	_, err := plugin.Resolve(t.Context(), identity, nil)
	r.Error(err)
	r.Contains(err.Error(), "no subject token")
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
		configKeyFlow:              flowTokenExchange,
		configKeyTokenURL:          srv.URL,
		configKeySubjectTokenEnvVar: "TEST_OIDC_PRIORITY_TOKEN",
		configKeySubjectToken:      "literal-token-value",
		configKeySubjectTokenType:  "urn:ietf:params:oauth:token-type:jwt",
		configKeyAudience:          "sigstore",
	}

	creds, err := plugin.Resolve(t.Context(), identity, nil)
	r.NoError(err)
	r.Equal("result-token", creds[credentialKeyToken])
}

func Test_OIDCPlugin_Resolve_TokenExchange_FileSource(t *testing.T) {
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
		configKeySubjectTokenType: "urn:ietf:params:oauth:token-type:jwt",
		configKeyAudience:         "sigstore",
	}

	creds, err := plugin.Resolve(t.Context(), identity, nil)
	r.NoError(err)
	r.Equal("file-result-token", creds[credentialKeyToken])
}
