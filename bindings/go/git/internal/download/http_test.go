package download

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuthenticatedHTTPClientRedirectPolicy(t *testing.T) {
	origin, err := url.Parse("https://git.example.com/repo")
	require.NoError(t, err)
	for _, tc := range []struct {
		name, target string
		allowed      bool
	}{
		{"same origin", "https://git.example.com/other", true},
		{"default port", "https://git.example.com:443/other", true},
		{"case insensitive host", "https://GIT.EXAMPLE.COM/other", true},
		{"child subdomain", "https://child.git.example.com/other", false},
		{"other host", "https://other.example.com/other", false},
		{"other port", "https://git.example.com:8443/other", false},
		{"downgrade", "http://git.example.com/other", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			original := &http.Client{}
			secured := authenticatedHTTPClient(original)
			target, err := url.Parse(tc.target)
			r.NoError(err)
			err = secured.CheckRedirect(&http.Request{URL: target}, []*http.Request{{URL: origin}})
			if tc.allowed {
				r.NoError(err)
			} else {
				r.EqualError(err, "authenticated git redirect changes origin")
			}
			r.Nil(original.CheckRedirect)
		})
	}
	t.Run("caller policy", func(t *testing.T) {
		r := require.New(t)
		sentinel := errors.New("caller rejects redirect")
		called := false
		original := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { called = true; return sentinel }}
		secured := authenticatedHTTPClient(original)
		r.ErrorIs(secured.CheckRedirect(&http.Request{URL: origin}, []*http.Request{{URL: origin}}), sentinel)
		r.True(called)
		r.NotNil(original.CheckRedirect)
	})
	t.Run("default redirect limit", func(t *testing.T) {
		r := require.New(t)
		via := make([]*http.Request, 10)
		for i := range via {
			via[i] = &http.Request{URL: origin}
		}
		r.EqualError(authenticatedHTTPClient(&http.Client{}).CheckRedirect(&http.Request{URL: origin}, via), "stopped after 10 redirects")
	})
}

func TestAuthenticatedHTTPClientRejectsCallerOriginChange(t *testing.T) {
	r := require.New(t)
	origin, err := url.Parse("https://git.example.com/repo")
	r.NoError(err)
	target, err := url.Parse("https://git.example.com/other")
	r.NoError(err)
	original := &http.Client{CheckRedirect: func(req *http.Request, _ []*http.Request) error { req.URL.Host = "other.example.com"; return nil }}
	r.EqualError(authenticatedHTTPClient(original).CheckRedirect(&http.Request{URL: target}, []*http.Request{{URL: origin}}), "authenticated git redirect changes origin")
}
