// Package spec defines the HTTP client configuration type
// http.config.ocm.software/v1alpha1.
//
// It lets OCM tune how outbound HTTP requests behave — primarily their
// timeouts. The configuration is a regular OCM config object, so it can
// live in any central .ocmconfig file alongside other configuration types.
//
// # Configuration type
//
// The type identifier is "http.config.ocm.software/v1alpha1". The
// unversioned alias "http.config.ocm.software" is also accepted, but
// deprecated. A minimal config looks like:
//
//	type: http.config.ocm.software/v1alpha1
//	timeout: 1m
//
// In a central .ocmconfig file the http config is nested inside the
// generic config envelope, which lets it sit alongside other config
// types in a single document:
//
//	type: generic.config.ocm.software/v1
//	configurations:
//	  - type: http.config.ocm.software/v1alpha1
//	    timeout: 2m
//	    tcpDialTimeout: 15s
//	    tcpKeepAlive: 30s
//	    tlsHandshakeTimeout: 5s
//	    responseHeaderTimeout: 10s
//	    idleConnTimeout: 60s
//
// LookupConfig filters this envelope for matching entries, decodes them
// into Config, and merges them (see "Merging" below).
//
// # Timeouts
//
// TimeoutConfig groups the individual timeout knobs. Every field is a
// human-readable duration string ("30s", "5m", "1h", ...) and every field
// is optional:
//
//   - timeout                — overall request budget: connect + TLS + headers + body (maps to http.Client.Timeout)
//   - tcpDialTimeout         — limit for establishing the TCP connection
//   - tcpKeepAlive           — interval between TCP keep-alive probes
//   - tlsHandshakeTimeout    — limit for the TLS handshake
//   - responseHeaderTimeout  — limit for receiving response headers after the request body is written
//   - idleConnTimeout        — how long an idle keep-alive connection stays in the pool
//
// The fields are pointers in Go. A nil pointer means "not configured —
// use the default", while an explicit zero ("0s") means "no timeout".
// This mirrors net/http, where a zero timeout disables the limit. The
// one exception is tcpKeepAlive: a negative value disables keep-alive
// probes (consistent with net.Dialer.KeepAlive) and is therefore not
// rejected by validation.
//
// When no config is supplied at all, LookupConfig falls back to
// DefaultTimeout (30s) for the overall timeout and leaves the rest unset.
//
// # Merging
//
// Multiple http configs may appear across config sources. Merge combines
// them with last-non-nil-wins semantics per timeout field. LookupConfig
// performs this merge and then applies the default timeout.
//
// # Validation
//
// Config.Validate — and the convenience wrapper ResolveHTTPConfig —
// rejects negative durations on every timeout field except tcpKeepAlive.
package spec
