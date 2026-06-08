// Package http builds HTTP clients from the http.config.ocm.software
// configuration type.
//
// # Quickstart
//
// Build a retry-enabled client from a central OCM config:
//
//	cfg, err := httpv1alpha1.ResolveHTTPConfig(genericConfig) // nil is OK → defaults
//	if err != nil {
//		return err
//	}
//	client := ocmhttp.New(
//		ocmhttp.WithConfig(cfg),
//		ocmhttp.WithUserAgent("my-tool/1.0"),
//	)
//
// # Config shape
//
// All timeout fields are pointers — nil means "keep http.DefaultTransport's
// value", a zero duration means "no timeout":
//
//	cfg := &httpv1alpha1.Config{
//		TimeoutConfig: httpv1alpha1.TimeoutConfig{
//			Timeout:               httpv1alpha1.NewTimeout(30 * time.Second), // whole request incl. body
//			TCPDialTimeout:        httpv1alpha1.NewTimeout(10 * time.Second),
//			TCPKeepAlive:          httpv1alpha1.NewTimeout(30 * time.Second),
//			TLSHandshakeTimeout:   httpv1alpha1.NewTimeout(10 * time.Second),
//			ResponseHeaderTimeout: httpv1alpha1.NewTimeout(10 * time.Second),
//			IdleConnTimeout:       httpv1alpha1.NewTimeout(90 * time.Second),
//		},
//	}
//
// # Per-host overrides
//
// Set cfg.Hosts to give individual hosts different timeouts. Fields left nil
// inherit the global value; set fields win:
//
//	cfg.Hosts = map[string]*httpv1alpha1.HostConfig{
//		"slow-registry.example.com": {
//			TimeoutConfig: httpv1alpha1.TimeoutConfig{
//				Timeout: httpv1alpha1.NewTimeout(120 * time.Second),
//			},
//		},
//		"internal.example.com:5000": { // host:port key wins over bare hostname
//			TimeoutConfig: httpv1alpha1.TimeoutConfig{
//				Timeout:             httpv1alpha1.NewTimeout(5 * time.Second),
//				ResponseHeaderTimeout: httpv1alpha1.NewTimeout(2 * time.Second),
//			},
//		},
//	}
//
// When Hosts is non-empty the per-host Timeout is enforced as a per-request
// context deadline (covering headers + body), so a per-host value can exceed
// the global. http.Client.Timeout is left zero in this case.
//
// # Lower-level constructors
//
// Skip the retry layer with NewClient, or get just the transport:
//
//	client    := ocmhttp.NewClient(cfg)
//	transport := ocmhttp.NewTransport(&cfg.TimeoutConfig)
package http
