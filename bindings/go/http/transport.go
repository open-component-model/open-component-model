package http

import (
	"net"
	nethttp "net/http"
	"time"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// Dialer defaults mirroring http.DefaultTransport's net.Dialer (see
// net/http.DefaultTransport in the Go source). They are used only when
// the caller sets TCPDialTimeout or TCPKeepAlive — see NewTransport.
const (
	defaultDialTimeout = 30 * time.Second
	defaultKeepAlive   = 30 * time.Second
)

// NewTransport returns an *http.Transport that starts as a clone of
// http.DefaultTransport and selectively overrides timeouts from cfg.
// A nil or empty cfg returns an unmodified clone of http.DefaultTransport.
//
// Setting TCPDialTimeout or TCPKeepAlive replaces the transport's DialContext
// with a fresh net.Dialer; http.Transport.Clone() does not expose the dialer
// behind http.DefaultTransport's DialContext, so it cannot be partially
// overridden. The replacement dialer starts from the same Timeout/KeepAlive
// defaults http.DefaultTransport uses (30s each, see defaultDialTimeout /
// defaultKeepAlive), and only the cfg fields that are non-nil overwrite them.
// Setting one TCP field without the other therefore leaves the other at that
// documented 30s default rather than at any value http.DefaultTransport's
// dialer happens to use today.
func NewTransport(cfg *httpv1alpha1.TimeoutConfig) *nethttp.Transport {
	dt, ok := nethttp.DefaultTransport.(*nethttp.Transport)
	if !ok {
		dt = &nethttp.Transport{}
	}
	transport := dt.Clone()

	if cfg == nil {
		return transport
	}

	if cfg.TCPDialTimeout != nil || cfg.TCPKeepAlive != nil {
		transport.DialContext = newDialer(cfg).DialContext
	}

	if cfg.TLSHandshakeTimeout != nil {
		transport.TLSHandshakeTimeout = time.Duration(*cfg.TLSHandshakeTimeout)
	}

	if cfg.ResponseHeaderTimeout != nil {
		transport.ResponseHeaderTimeout = time.Duration(*cfg.ResponseHeaderTimeout)
	}

	if cfg.IdleConnTimeout != nil {
		transport.IdleConnTimeout = time.Duration(*cfg.IdleConnTimeout)
	}

	return transport
}

// newDialer builds the net.Dialer used to replace DialContext when the
// caller sets either TCP-level timeout. The dialer starts from the same
// defaults http.DefaultTransport uses (defaultDialTimeout / defaultKeepAlive)
// and the non-nil fields on cfg override them. It is unexported and tested
// via dialer_internal_test.go (an internal test in package http).
func newDialer(cfg *httpv1alpha1.TimeoutConfig) *net.Dialer {
	dialer := &net.Dialer{
		Timeout:   defaultDialTimeout,
		KeepAlive: defaultKeepAlive,
	}
	if cfg.TCPDialTimeout != nil {
		dialer.Timeout = time.Duration(*cfg.TCPDialTimeout)
	}
	if cfg.TCPKeepAlive != nil {
		dialer.KeepAlive = time.Duration(*cfg.TCPKeepAlive)
	}
	return dialer
}

// NewClient returns an *http.Client whose Transport is produced by NewTransport
// and whose Timeout reflects cfg.Timeout. A nil cfg.Timeout leaves the client
// with no overall deadline (Go's http.Client default). A nil cfg returns an
// *http.Client with a default Transport and no Timeout.
//
// When cfg carries per-host entries, the transport is fronted by a routing
// layer that dispatches each request to a transport built from the host's
// merged TimeoutConfig and applies the host's overall Timeout via a context
// deadline. http.Client.Timeout is left zero in that case so a per-host
// timeout can exceed the global.
//
// cfg.Timeout (when set without per-host entries) is the overall http.Client
// deadline and is independent of the transport-level timeouts; setting
// Timeout alone does NOT also configure TCPDialTimeout, TLSHandshakeTimeout,
// etc.
//
// NewClient produces a plain client with no retry behaviour. For the
// retry-enabled client used for OCI registry traffic, use New.
func NewClient(cfg *httpv1alpha1.Config) *nethttp.Client {
	build := func(tc *httpv1alpha1.TimeoutConfig) nethttp.RoundTripper {
		return NewTransport(tc)
	}
	return &nethttp.Client{
		Transport: buildRoutingTransport(cfg, build),
		Timeout:   clientLevelTimeout(cfg),
	}
}
