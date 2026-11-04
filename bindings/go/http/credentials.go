package http

import (
	"context"
	"fmt"
	nethttp "net/http"
	"net/url"
	"slices"
	"strings"
)

// Credential sets the authorization a request carries. It is handed a per-hop
// clone of the request, so an implementation may set headers but must not
// retain or modify anything else.
type Credential func(*nethttp.Request)

// BasicAuth returns a Credential that sends username and password as HTTP
// basic authorization.
func BasicAuth(username, password string) Credential {
	return func(req *nethttp.Request) {
		req.SetBasicAuth(username, password)
	}
}

// BearerToken returns a Credential that sends token as a bearer authorization.
func BearerToken(token string) Credential {
	return func(req *nethttp.Request) {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// CredentialSource reports which credential a request addressed to host must
// carry, host being its "host" or "host:port". A nil Credential sends the
// request unauthenticated.
//
// The context is the request's own, so a source may read request-scoped state
// from it — a credential resolver, for instance, whose lifetime is that of the
// operation rather than of the client.
type CredentialSource func(ctx context.Context, host string) (Credential, error)

// perHostHeaders are the request headers this layer owns. They are the ones
// net/http itself treats as bound to a single host and drops when it follows a
// cross-domain redirect; a transport that injects them on each hop instead of
// setting them on the original request bypasses that, so they are dropped here
// and reapplied only for the host they belong to.
var perHostHeaders = []string{"Authorization", "Cookie", "Cookie2"}

// WithCredentialSource returns a copy of client that asks src, for every
// request, which credential it must carry.
//
// The question is asked per hop rather than per call, because net/http
// re-enters the transport chain for every redirect it follows: a redirect to
// another host arrives here as a request to that host and is served whatever
// src holds for it, which for an unknown host is nothing. A
// credential-injecting transport layered above this one — go-github's auth
// transport, for instance — re-adds its header on the redirected request and
// would otherwise defeat net/http's own stripping; this layer owning the
// per-host headers is what catches it.
//
// The returned client shares client's transport and therefore its connection
// pool, so a source can be installed per download without costing a new pool.
// A nil client is treated as one built by New.
func WithCredentialSource(client *nethttp.Client, src CredentialSource) *nethttp.Client {
	if client == nil {
		client = New()
	}
	base := client.Transport
	if base == nil {
		base = nethttp.DefaultTransport
	}
	c := *client
	c.Transport = &credentialSourceTransport{base: base, src: src}
	return &c
}

// BindCredential returns a copy of client that carries cred to hosts and
// nothing to any other host. It is WithCredentialSource with StaticCredential,
// for a credential already resolved for a known host.
func BindCredential(client *nethttp.Client, cred Credential, hosts ...string) *nethttp.Client {
	return WithCredentialSource(client, StaticCredential(cred, hosts...))
}

// StaticCredential returns a CredentialSource serving cred to hosts and
// nothing elsewhere. An empty hosts list serves nothing at all.
func StaticCredential(cred Credential, hosts ...string) CredentialSource {
	bound := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if h := hostnameOf(host); h != "" {
			bound = append(bound, h)
		}
	}
	return func(_ context.Context, host string) (Credential, error) {
		h := hostnameOf(host)
		if slices.ContainsFunc(bound, func(b string) bool { return servedBy(h, b) }) {
			return cred, nil
		}
		return nil, nil
	}
}

// BoundToHost reports whether a request to requestHost is served by a
// credential resolved for host: the same host, or one of its subdomains,
// because a host's subdomains are served by the same operator — a GitHub token
// is configured for github.com while the API lives on api.github.com. The port
// is not compared, a host serving its API on another port being the same
// operator too.
func BoundToHost(host, requestHost string) bool {
	return servedBy(hostnameOf(requestHost), hostnameOf(host))
}

// credentialSourceTransport asks src which credential each request carries,
// and owns the per-host headers while doing so. See WithCredentialSource.
type credentialSourceTransport struct {
	base nethttp.RoundTripper
	src  CredentialSource
}

func (t *credentialSourceTransport) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	req = req.Clone(req.Context())
	cred, err := t.src(req.Context(), req.URL.Host)
	if err != nil {
		return nil, fmt.Errorf("error resolving credentials for %q: %w", req.URL.Host, err)
	}
	// Drop first: the header may have been injected above this layer for the
	// host the call started on rather than the one this hop addresses.
	for _, header := range perHostHeaders {
		req.Header.Del(header)
	}
	if cred != nil {
		cred(req)
	}
	return t.base.RoundTrip(req)
}

// servedBy reports whether host is bound or one of its subdomains. Both must
// already be normalized by hostnameOf.
func servedBy(host, bound string) bool {
	if host == "" || bound == "" {
		return false
	}
	return host == bound || strings.HasSuffix(host, "."+bound)
}

// hostnameOf strips an optional port and IPv6 brackets from host and
// lowercases the rest, so that "Host", "host:443" and "[::1]:80" compare as
// the hosts they address.
func hostnameOf(host string) string {
	u := url.URL{Host: strings.TrimSpace(host)}
	return strings.ToLower(u.Hostname())
}
