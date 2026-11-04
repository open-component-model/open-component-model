package http

import nethttp "net/http"

// UnwrapReplayable returns the RoundTripper wrapped by a replayableBodyTransport,
// or rt itself, so external tests can inspect the configured chain below it.
func UnwrapReplayable(rt nethttp.RoundTripper) nethttp.RoundTripper {
	if r, ok := rt.(*replayableBodyTransport); ok {
		return r.base
	}
	return rt
}
