package http

import (
	"crypto/tls"
	"fmt"
	nethttp "net/http"
	"slices"

	"ocm.software/open-component-model/bindings/go/http/internal/retry"
)

// CloneWithTLS returns a copy of client whose TLS configuration is adjusted by
// apply, e.g. to present a client certificate. Every [nethttp.Transport] in the
// transport chain built by [New] is cloned and its TLS configuration passed to
// apply, so timeouts, retries, per-host routing, proxy and compression settings
// are kept. client itself is not modified.
//
// A nil transport is treated as [nethttp.DefaultTransport]. A transport chain
// containing a [nethttp.RoundTripper] not built by this package is rejected
// instead of being replaced, because replacing it would silently drop its
// behaviour.
func CloneWithTLS(client *nethttp.Client, apply func(*tls.Config)) (*nethttp.Client, error) {
	rt := client.Transport
	if rt == nil {
		rt = nethttp.DefaultTransport
	}
	cloned, err := cloneTransportWithTLS(rt, apply)
	if err != nil {
		return nil, err
	}
	c := *client
	c.Transport = cloned
	return &c, nil
}

func cloneTransportWithTLS(rt nethttp.RoundTripper, apply func(*tls.Config)) (nethttp.RoundTripper, error) {
	switch t := rt.(type) {
	case *nethttp.Transport:
		c := t.Clone()
		if c.TLSClientConfig == nil {
			c.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		// Transport.Clone copies the TLS configuration shallowly; give apply its
		// own certificate pool and list so it cannot change the original client.
		if c.TLSClientConfig.RootCAs != nil {
			c.TLSClientConfig.RootCAs = c.TLSClientConfig.RootCAs.Clone()
		}
		c.TLSClientConfig.Certificates = slices.Clone(c.TLSClientConfig.Certificates)
		apply(c.TLSClientConfig)
		return c, nil
	case *retry.Transport:
		base := t.Base
		if base == nil {
			base = nethttp.DefaultTransport
		}
		b, err := cloneTransportWithTLS(base, apply)
		if err != nil {
			return nil, err
		}
		return &retry.Transport{Base: b, Policy: t.Policy}, nil
	case *insecureWarnTransport:
		b, err := cloneTransportWithTLS(t.base, apply)
		if err != nil {
			return nil, err
		}
		return &insecureWarnTransport{base: b}, nil
	case *userAgentTransport:
		b, err := cloneTransportWithTLS(t.base, apply)
		if err != nil {
			return nil, err
		}
		return &userAgentTransport{base: b, userAgent: t.userAgent}, nil
	case *hostRouter:
		global, err := cloneTransportWithTLS(t.globalRT, apply)
		if err != nil {
			return nil, err
		}
		hosts := make(map[string]nethttp.RoundTripper, len(t.hosts))
		for host, hrt := range t.hosts {
			if hosts[host], err = cloneTransportWithTLS(hrt, apply); err != nil {
				return nil, err
			}
		}
		return &hostRouter{globalRT: global, globalTimeout: t.globalTimeout, hosts: hosts, hostTimeouts: t.hostTimeouts}, nil
	case errorRoundTripper:
		// A misconfigured chain keeps failing every request.
		return t, nil
	default:
		return nil, fmt.Errorf("cannot configure TLS on HTTP transport of type %T", rt)
	}
}
