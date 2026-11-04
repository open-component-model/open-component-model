package http

import (
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

// perHostHeaders are dropped from every request leaving the hosts a credential
// is bound to. They are the request headers net/http itself treats as bound to
// one host and drops when it follows a cross-domain redirect; a transport that
// injects them on each hop instead of setting them on the original request
// bypasses that, so they are dropped here too.
var perHostHeaders = []string{"Authorization", "Cookie", "Cookie2"}

// BindCredential returns a copy of client whose requests carry cred only while
// their URL host is one of hosts, and which strips the per-host headers from
// every request to any other host.
//
// The decision is taken per hop rather than per call, because net/http
// re-enters the transport chain for every redirect it follows: a redirect
// leaving hosts arrives here with the new host and loses the credential. A
// credential-injecting transport layered above this one — go-github's auth
// transport, for instance — re-adds its header on the redirected request and
// would otherwise defeat net/http's own stripping; stripping again here is
// what catches it.
//
// A hosts entry is a "host" or "host:port"; the port is ignored, and a request
// host that is a subdomain of an entry matches it, because the credential was
// resolved for a host whose subdomains are served by the same operator — a
// GitHub token is configured for github.com while the API lives on
// api.github.com. An empty hosts list binds the credential to nothing, so
// every request goes out unauthenticated.
//
// The returned client shares client's transport and therefore its connection
// pool, so binding a credential per download does not cost a new pool. A nil
// client is treated as one built by New.
func BindCredential(client *nethttp.Client, cred Credential, hosts ...string) *nethttp.Client {
	if client == nil {
		client = New()
	}
	base := client.Transport
	if base == nil {
		base = nethttp.DefaultTransport
	}

	bound := make([]string, 0, len(hosts))
	for _, host := range hosts {
		if h := hostnameOf(host); h != "" {
			bound = append(bound, h)
		}
	}

	c := *client
	c.Transport = &boundCredentialTransport{base: base, hosts: bound, cred: cred}
	return &c
}

// BoundToHost reports whether a credential bound to host by BindCredential is
// sent to requestHost. Callers use it to refuse a configuration that would
// address another host than the credential was resolved for, instead of
// letting the request go out unauthenticated and fail as a missing permission.
func BoundToHost(host, requestHost string) bool {
	return servedBy(hostnameOf(requestHost), hostnameOf(host))
}

// boundCredentialTransport attaches cred to requests for one of hosts and
// strips the per-host headers from requests to any other host. See
// BindCredential.
type boundCredentialTransport struct {
	base  nethttp.RoundTripper
	hosts []string
	cred  Credential
}

func (t *boundCredentialTransport) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	req = req.Clone(req.Context())
	host := hostnameOf(req.URL.Host)
	if slices.ContainsFunc(t.hosts, func(bound string) bool { return servedBy(host, bound) }) {
		t.cred(req)
	} else {
		for _, header := range perHostHeaders {
			req.Header.Del(header)
		}
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
