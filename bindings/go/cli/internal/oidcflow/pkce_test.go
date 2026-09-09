package oidcflow

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/stretchr/testify/require"
)

func Test_pkce(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	h := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(h[:])

	p := &pkce{challenge: challenge, verifier: verifier}
	r.Len(p.authURLOpts(), 2)
	r.Len(p.tokenURLOpts(), 1)
	r.Len(challenge, 43)
	r.NotEqual(verifier, challenge)
}

func Test_newPKCE(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		methods        string
		wantErr        string
		wantNonEmpty   bool
		wantChallenges bool
	}{
		{
			name:           "S256 supported",
			methods:        `["S256", "plain"]`,
			wantNonEmpty:   true,
			wantChallenges: true,
		},
		{
			name:    "only plain rejected",
			methods: `["plain"]`,
			wantErr: "does not support PKCE S256",
		},
		{
			name:    "empty list rejected",
			methods: `[]`,
			wantErr: "does not support PKCE S256",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			var srvURL string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{
					"issuer": %q,
					"authorization_endpoint": %q,
					"token_endpoint": %q,
					"jwks_uri": %q,
					"response_types_supported": ["code"],
					"subject_types_supported": ["public"],
					"id_token_signing_alg_values_supported": ["RS256"],
					"code_challenge_methods_supported": %s
				}`, srvURL, srvURL+"/auth", srvURL+"/token", srvURL+"/keys", tt.methods)
			}))
			defer srv.Close()
			srvURL = srv.URL

			provider, err := oidc.NewProvider(t.Context(), srv.URL)
			r.NoError(err)

			p, err := newPKCE(provider)
			if tt.wantErr != "" {
				r.Error(err)
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.NotEmpty(p.verifier)
			r.NotEmpty(p.challenge)
			r.NotEqual(p.verifier, p.challenge)

			// Challenge must be the SHA-256 of the verifier, base64url-no-pad.
			h := sha256.Sum256([]byte(p.verifier))
			r.Equal(base64.RawURLEncoding.EncodeToString(h[:]), p.challenge)

			// Auth URL options must carry both the method and the challenge.
			r.Len(p.authURLOpts(), 2)
			r.Len(p.tokenURLOpts(), 1)
		})
	}
}
