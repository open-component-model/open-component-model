package http

import (
	"log/slog"
	nethttp "net/http"
	"sync"
)

// insecureWarnTransport wraps an http.RoundTripper and emits a slog.WarnContext
// on the first request when InsecureSkipVerify is active.
// Each instance is scoped to a single host chain (global or per-host) by
// buildRoutingTransport, so a single sync.Once is sufficient.
type insecureWarnTransport struct {
	base nethttp.RoundTripper
	once sync.Once
}

func (t *insecureWarnTransport) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	t.once.Do(func() {
		slog.WarnContext(req.Context(),
			"TLS certificate verification disabled (InsecureSkipVerify=true) — connections are vulnerable to MITM attacks",
			"host", req.URL.Host)
	})
	return t.base.RoundTrip(req)
}
