package http_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	nethttp "net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
)

// seenAuth records the Authorization header of every request, keyed by the host
// it was addressed to.
type seenAuth map[string]string

func (s seenAuth) record(req *nethttp.Request) {
	s[req.Host] = req.Header.Get("Authorization")
}

func (s seenAuth) handler() nethttp.HandlerFunc {
	return func(w nethttp.ResponseWriter, req *nethttp.Request) {
		s.record(req)
		w.WriteHeader(nethttp.StatusOK)
	}
}

// clientDialing returns a client whose requests are dialled according to hosts,
// a mapping from the "host:port" of a request URL to the address actually
// connected to. It lets a test address servers by distinct hostnames although
// every httptest server listens on 127.0.0.1.
func clientDialing(hosts map[string]string) *nethttp.Client {
	return &nethttp.Client{Transport: &nethttp.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if target, ok := hosts[addr]; ok {
				addr = target
			}
			var dialer net.Dialer
			return dialer.DialContext(ctx, network, addr)
		},
	}}
}

func TestBindCredential(t *testing.T) {
	t.Run("sends the credential to the bound host", func(t *testing.T) {
		r := require.New(t)
		seen := seenAuth{}
		server := httptest.NewServer(seen.handler())
		defer server.Close()

		u, err := url.Parse(server.URL)
		r.NoError(err)
		client := ocmhttp.BindCredential(server.Client(), ocmhttp.BearerToken("secret"), u.Host)

		resp, err := get(t, client, server.URL)
		r.NoError(err)
		r.NoError(resp.Body.Close())
		r.Equal("Bearer secret", seen[u.Host])
	})

	t.Run("withholds the credential from another host", func(t *testing.T) {
		r := require.New(t)
		seen := seenAuth{}
		server := httptest.NewServer(seen.handler())
		defer server.Close()

		u, err := url.Parse(server.URL)
		r.NoError(err)
		client := ocmhttp.BindCredential(server.Client(), ocmhttp.BasicAuth("victim", "password"), "other.invalid")

		resp, err := get(t, client, server.URL)
		r.NoError(err)
		r.NoError(resp.Body.Close())
		r.Empty(seen[u.Host])
	})

	t.Run("strips a credential an outer transport injected for another host", func(t *testing.T) {
		r := require.New(t)
		seen := seenAuth{}
		server := httptest.NewServer(seen.handler())
		defer server.Close()

		u, err := url.Parse(server.URL)
		r.NoError(err)
		client := ocmhttp.BindCredential(server.Client(), ocmhttp.BearerToken("secret"), "other.invalid")
		// A library that authenticates in its own transport — go-github does —
		// re-adds the header on every hop, so net/http's own cross-host
		// stripping never sees it.
		bound := client.Transport
		client.Transport = roundTripperFunc(func(req *nethttp.Request) (*nethttp.Response, error) {
			req = req.Clone(req.Context())
			req.Header.Set("Authorization", "Bearer injected")
			return bound.RoundTrip(req)
		})

		resp, err := get(t, client, server.URL)
		r.NoError(err)
		r.NoError(resp.Body.Close())
		r.Empty(seen[u.Host], "the injected header must not reach a host the credential is not bound to")
	})

	t.Run("drops the credential on a redirect to another host", func(t *testing.T) {
		r := require.New(t)
		seen := seenAuth{}

		elsewhere := httptest.NewServer(seen.handler())
		defer elsewhere.Close()
		bound := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, req *nethttp.Request) {
			seen.record(req)
			nethttp.Redirect(w, req, "http://elsewhere.invalid/archive", nethttp.StatusFound)
		}))
		defer bound.Close()

		client := clientDialing(map[string]string{
			"bound.invalid:80":     serverAddr(t, bound),
			"elsewhere.invalid:80": serverAddr(t, elsewhere),
		})
		client = ocmhttp.BindCredential(client, ocmhttp.BearerToken("secret"), "bound.invalid")

		resp, err := get(t, client, "http://bound.invalid/commits/main")
		r.NoError(err)
		r.NoError(resp.Body.Close())
		r.Equal("Bearer secret", seen["bound.invalid"])
		r.Empty(seen["elsewhere.invalid"], "the credential must not follow a redirect off the bound host")
	})

	t.Run("leaves the supplied client untouched", func(t *testing.T) {
		r := require.New(t)
		base := ocmhttp.New()
		transport := base.Transport
		bound := ocmhttp.BindCredential(base, ocmhttp.BearerToken("secret"), "example.invalid")
		r.NotSame(base, bound)
		r.Same(transport, base.Transport, "binding a credential must not re-wire the shared client")
	})
}

func TestWithCredentialSource(t *testing.T) {
	t.Run("serves each host what the source holds for it", func(t *testing.T) {
		r := require.New(t)
		seen := seenAuth{}
		first := httptest.NewServer(seen.handler())
		defer first.Close()
		second := httptest.NewServer(seen.handler())
		defer second.Close()

		client := clientDialing(map[string]string{
			"first.invalid:80":  serverAddr(t, first),
			"second.invalid:80": serverAddr(t, second),
		})
		client = ocmhttp.WithCredentialSource(client, func(_ context.Context, host string) (ocmhttp.Credential, error) {
			switch host {
			case "first.invalid":
				return ocmhttp.BearerToken("first-token"), nil
			case "second.invalid":
				return ocmhttp.BearerToken("second-token"), nil
			}
			return nil, nil
		})

		for _, url := range []string{"http://first.invalid/a", "http://second.invalid/b"} {
			resp, err := get(t, client, url)
			r.NoError(err)
			r.NoError(resp.Body.Close())
		}
		r.Equal("Bearer first-token", seen["first.invalid"])
		r.Equal("Bearer second-token", seen["second.invalid"])
	})

	t.Run("asks for the redirect target, not the host the call started on", func(t *testing.T) {
		r := require.New(t)
		seen := seenAuth{}
		var asked []string

		elsewhere := httptest.NewServer(seen.handler())
		defer elsewhere.Close()
		start := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, req *nethttp.Request) {
			seen.record(req)
			nethttp.Redirect(w, req, "http://elsewhere.invalid/chart", nethttp.StatusFound)
		}))
		defer start.Close()

		client := clientDialing(map[string]string{
			"start.invalid:80":     serverAddr(t, start),
			"elsewhere.invalid:80": serverAddr(t, elsewhere),
		})
		client = ocmhttp.WithCredentialSource(client, func(_ context.Context, host string) (ocmhttp.Credential, error) {
			asked = append(asked, host)
			// The redirect target has its own credential configured, as an
			// ocmconfig entry for that host would give it.
			if host == "elsewhere.invalid" {
				return ocmhttp.BasicAuth("user", "elsewhere-password"), nil
			}
			return ocmhttp.BearerToken("start-token"), nil
		})

		resp, err := get(t, client, "http://start.invalid/chart")
		r.NoError(err)
		r.NoError(resp.Body.Close())
		r.Equal([]string{"start.invalid", "elsewhere.invalid"}, asked)
		r.Equal("Bearer start-token", seen["start.invalid"])
		r.Equal(nethttp.Header{"Authorization": []string{"Basic " + base64.StdEncoding.EncodeToString([]byte("user:elsewhere-password"))}}.Get("Authorization"),
			seen["elsewhere.invalid"], "the redirect target must get its own credential")
	})

	t.Run("fails the request when the source fails", func(t *testing.T) {
		r := require.New(t)
		server := httptest.NewServer(seenAuth{}.handler())
		defer server.Close()

		client := ocmhttp.WithCredentialSource(server.Client(), func(_ context.Context, _ string) (ocmhttp.Credential, error) {
			return nil, errors.New("resolver exploded")
		})
		_, err := get(t, client, server.URL)
		r.ErrorContains(err, "resolver exploded")
	})
}

func TestBoundToHost(t *testing.T) {
	for _, tc := range []struct {
		name        string
		host        string
		requestHost string
		want        bool
	}{
		{name: "same host", host: "github.com", requestHost: "github.com", want: true},
		{name: "api subdomain", host: "github.com", requestHost: "api.github.com", want: true},
		{name: "upload subdomain", host: "github.com", requestHost: "uploads.github.com", want: true},
		{name: "port is ignored", host: "ghe.example:443", requestHost: "ghe.example", want: true},
		{name: "case is ignored", host: "GHE.example", requestHost: "ghe.example", want: true},
		{name: "unrelated host", host: "github.com", requestHost: "attacker.invalid", want: false},
		{name: "suffix is not a subdomain", host: "github.com", requestHost: "github.com.attacker.invalid", want: false},
		{name: "parent of the bound host", host: "api.github.com", requestHost: "github.com", want: false},
		{name: "empty bound host", host: "", requestHost: "github.com", want: false},
		{name: "empty request host", host: "github.com", requestHost: "", want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.New(t).Equal(tc.want, ocmhttp.BoundToHost(tc.host, tc.requestHost))
		})
	}
}

// get issues a GET bound to the test's context, which (*http.Client).Get is not.
func get(t *testing.T, client *nethttp.Client, url string) (*nethttp.Response, error) {
	t.Helper()
	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, url, nil)
	require.NoError(t, err)
	return client.Do(req)
}

func serverAddr(t *testing.T, server *httptest.Server) string {
	t.Helper()
	u, err := url.Parse(server.URL)
	require.NoError(t, err)
	return u.Host
}
