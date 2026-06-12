package http

import (
	nethttp "net/http"
	"time"

	"ocm.software/open-component-model/bindings/go/http/internal/retry"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// Options holds configuration for New.
type Options struct {
	config    *httpv1alpha1.Config
	userAgent string
}

// Option is a functional option for New.
type Option func(*Options)

// WithConfig sets the HTTP configuration (timeouts) used to build the client.
func WithConfig(cfg *httpv1alpha1.Config) Option {
	return func(o *Options) {
		o.config = cfg
	}
}

// WithUserAgent sets the User-Agent header injected on every request.
// The supplied value always overwrites any User-Agent the caller sets on
// individual requests; it is intended for library-level identification
// (e.g. "ocm-cli/1.2.3") rather than per-request customisation.
func WithUserAgent(userAgent string) Option {
	return func(o *Options) {
		o.userAgent = userAgent
	}
}

// userAgentTransport wraps an http.RoundTripper and injects a User-Agent header.
type userAgentTransport struct {
	base      nethttp.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", t.userAgent)
	return t.base.RoundTrip(req)
}

// retryPolicyFromConfig translates a *RetryConfig into a retry.Policy.
// Returns nil when rc is nil, which causes retry.Transport to use DefaultGenericPolicy.
func retryPolicyFromConfig(rc *httpv1alpha1.RetryConfig) retry.Policy {
	if rc == nil {
		return nil
	}
	maxRetries := retry.DefaultGenericPolicy.MaxRetry
	minWait := retry.DefaultGenericPolicy.MinWait
	maxWait := retry.DefaultGenericPolicy.MaxWait
	if rc.MaxRetries != nil {
		maxRetries = *rc.MaxRetries
	}
	if rc.MinWait != nil {
		minWait = time.Duration(*rc.MinWait)
	}
	if rc.MaxWait != nil {
		maxWait = time.Duration(*rc.MaxWait)
	}
	return retry.NewGenericPolicy(maxRetries, minWait, maxWait)
}

// New builds an *http.Client with retry transport, applying the
// transport-level timeouts from the supplied HTTP configuration. It is the
// factory counterpart to httpv1alpha1.ResolveHTTPConfig: resolve the config
// once, then hand it here to obtain a ready-to-use client.
//
// Transport chain (outermost first):
//
//		http.Client → [userAgentTransport] → [hostRouter] → [insecureWarnTransport] → retry.Transport → http.Transport
//
//	 1. userAgentTransport sets the User-Agent header (only when WithUserAgent
//	    is given).
//	 2. hostRouter dispatches each request to a per-host inner chain when the
//	    URL host matches an entry in cfg.Hosts; otherwise it falls back to the
//	    global chain. Omitted entirely when cfg has no per-host entries.
//	 3. insecureWarnTransport (only when InsecureSkipVerify=true) emits a
//	    slog.WarnContext on the first request per host.
//	 4. retry.Transport retries transient failures using the default retry
//	    policy. One instance exists per host (plus one for the global fallback)
//	    so retry attempts share the per-host context deadline.
//	 5. http.Transport carries the configured TCP/TLS/idle timeouts, merged
//	    from the global config and the matching per-host overrides.
//
// Without per-host entries, the overall Timeout is applied as
// http.Client.Timeout. With per-host entries it is applied per request inside
// hostRouter via a context deadline, so a per-host timeout may exceed the
// global one.
//
// A nil config (WithConfig omitted, or omitted entirely) yields a plain
// retry client with default transport timeouts.
func New(opts ...Option) *nethttp.Client {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}

	build := func(tc *httpv1alpha1.TimeoutConfig, rc *httpv1alpha1.RetryConfig, tlsc *httpv1alpha1.TLSConfig) nethttp.RoundTripper {
		rt := nethttp.RoundTripper(retry.NewTransport(NewTransportWithTLS(tc, tlsc)))
		if p := retryPolicyFromConfig(rc); p != nil {
			rt.(*retry.Transport).Policy = func() retry.Policy { return p }
		}
		if tlsc != nil && tlsc.InsecureSkipVerify != nil && *tlsc.InsecureSkipVerify {
			rt = &insecureWarnTransport{base: rt}
		}
		return rt
	}

	httpClient := &nethttp.Client{Transport: buildRoutingTransport(options.config, build)}
	httpClient.Timeout = clientLevelTimeout(options.config)

	if options.userAgent != "" {
		httpClient.Transport = &userAgentTransport{
			base:      httpClient.Transport,
			userAgent: options.userAgent,
		}
	}

	return httpClient
}

// buildRoutingTransport builds the transport chain that fronts every request
// from a client built out of cfg. inner is invoked for each TimeoutConfig,
// RetryConfig, and TLSConfig (global, then once per host entry) to produce the
// innermost RoundTripper — callers use that to layer retry, instrumentation, etc.
//
// When cfg has no per-host entries the result is whatever inner returned for
// the global config. With per-host entries the result is a hostRouter that
// dispatches to a per-host inner chain, applying the per-host overall
// Timeout via a context deadline so a per-host timeout can exceed the global.
func buildRoutingTransport(cfg *httpv1alpha1.Config, inner func(*httpv1alpha1.TimeoutConfig, *httpv1alpha1.RetryConfig, *httpv1alpha1.TLSConfig) nethttp.RoundTripper) nethttp.RoundTripper {
	if cfg == nil {
		return inner(nil, nil, nil)
	}

	globalChain := inner(&cfg.TimeoutConfig, cfg.Retry, &cfg.TLSConfig)
	if len(cfg.Hosts) == 0 {
		return globalChain
	}

	var globalTimeout time.Duration
	if cfg.Timeout != nil {
		globalTimeout = time.Duration(*cfg.Timeout)
	}

	hosts := make(map[string]nethttp.RoundTripper, len(cfg.Hosts))
	hostTimeouts := make(map[string]time.Duration, len(cfg.Hosts))
	for host, hc := range cfg.Hosts {
		if hc == nil {
			continue
		}
		merged := httpv1alpha1.MergeTimeoutConfig(&cfg.TimeoutConfig, &hc.TimeoutConfig)
		mergedRetry := httpv1alpha1.MergeRetryConfig(cfg.Retry, hc.Retry)
		mergedTLS := httpv1alpha1.MergeTLSConfig(&cfg.TLSConfig, &hc.TLSConfig)
		hosts[host] = inner(&merged, mergedRetry, &mergedTLS)
		if merged.Timeout != nil {
			hostTimeouts[host] = time.Duration(*merged.Timeout)
		}
	}

	// All host entries were nil — behave as if no hosts were configured.
	if len(hosts) == 0 {
		return globalChain
	}

	return &hostRouter{
		globalRT:      globalChain,
		globalTimeout: globalTimeout,
		hosts:         hosts,
		hostTimeouts:  hostTimeouts,
	}
}

// clientLevelTimeout returns the value to set on http.Client.Timeout. With
// active per-host routing, the overall timeout is applied per request inside
// hostRouter — setting http.Client.Timeout would cap every request at the
// global value before the host override could take effect.
// When all host entries are nil, routing falls back to the global chain, so
// the global Timeout is safe to apply as http.Client.Timeout.
func clientLevelTimeout(cfg *httpv1alpha1.Config) time.Duration {
	if cfg == nil || cfg.Timeout == nil {
		return 0
	}
	for _, hc := range cfg.Hosts {
		if hc != nil {
			return 0
		}
	}
	return time.Duration(*cfg.Timeout)
}
