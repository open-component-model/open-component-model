// Package http builds HTTP transports and clients from the
// http.config.ocm.software configuration type.
//
// It is the construction-side companion to the configuration package
// ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1,
// which owns the Config type itself.
//
// # Usage
//
// Resolve the HTTP configuration from a central OCM config, then build a
// client from it:
//
//	import (
//		httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
//		ocmhttp "ocm.software/open-component-model/bindings/go/http"
//	)
//
//	// genericConfig is the central *genericv1.Config; it may be nil, in
//	// which case ResolveHTTPConfig returns a Config with default timeouts.
//	cfg, err := httpv1alpha1.ResolveHTTPConfig(genericConfig)
//	if err != nil {
//		return err // configuration was present but invalid
//	}
//
//	client := ocmhttp.New(
//		ocmhttp.WithConfig(cfg),
//		ocmhttp.WithUserAgent("my-component/1.0"),
//	)
//
// New returns an *http.Client whose transport applies the TCP dial, TCP
// keep-alive, TLS-handshake, response-header and idle-connection timeouts
// from cfg, layered on top of oras-go's retry transport. The overall
// cfg.Timeout becomes the http.Client.Timeout (the deadline for the whole
// request, including redirects and body reads). WithUserAgent injects a
// User-Agent header on every request. A nil config yields a plain retry
// client with library defaults.
//
// When the retry wrapper is not wanted, NewClient and NewTransport build a
// bare *http.Client / *http.Transport directly from the configuration:
//
//	transport := ocmhttp.NewTransport(&cfg.TimeoutConfig)
//	client := ocmhttp.NewClient(cfg)
//
// The configuration distinguishes nil ("use the default") from a zero
// duration ("no timeout"), matching net/http semantics, so leaving a field
// unset preserves http.DefaultTransport's behaviour for it.
//
// # Per-host overrides
//
// When cfg.Hosts is non-empty, both New and NewClient front the transport
// with a routing layer. Each request is dispatched to a transport built from
// the host's merged TimeoutConfig (global + per-host overrides) when the
// request URL's host matches an entry in cfg.Hosts; otherwise it goes to the
// transport built from the global config.
//
// Map keys may be either "host" or "host:port". The full Host is matched
// first, falling back to the bare hostname when the URL carries an explicit
// port that does not appear as a key.
//
// The overall Timeout is applied per request via a context deadline so that
// a per-host timeout can exceed the global value — setting http.Client.Timeout
// would cap every request at the global before the host override could take
// effect. http.Client.Timeout is therefore left zero whenever cfg.Hosts is
// non-empty.
package http
