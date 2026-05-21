// Package httpclient builds HTTP transports and clients from the
// http.config.ocm.software configuration type.
//
// It is the construction-side companion to the configuration package
// ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec,
// which owns the Config type itself.
//
// # Usage
//
// Resolve the HTTP configuration from a central OCM config, then build a
// client from it:
//
//	import (
//		httpv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/http/v1alpha1/spec"
//		"ocm.software/open-component-model/bindings/go/httpclient"
//	)
//
//	// genericConfig is the central *genericv1.Config; it may be nil, in
//	// which case ResolveHTTPConfig returns a Config with default timeouts.
//	cfg, err := httpv1alpha1.ResolveHTTPConfig(genericConfig)
//	if err != nil {
//		return err // configuration was present but invalid
//	}
//
//	client := httpclient.New(
//		httpclient.WithConfig(cfg),
//		httpclient.WithUserAgent("my-component/1.0"),
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
//	transport := httpclient.NewTransport(&cfg.TimeoutConfig)
//	client := httpclient.NewClient(cfg)
//
// The configuration distinguishes nil ("use the default") from a zero
// duration ("no timeout"), matching net/http semantics, so leaving a field
// unset preserves http.DefaultTransport's behaviour for it.
package httpclient
