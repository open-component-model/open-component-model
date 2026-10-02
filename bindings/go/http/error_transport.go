package http

import (
	nethttp "net/http"
)

// errorRoundTripper is a RoundTripper that fails every request with a fixed
// error. It is used to defer a transport-construction failure (for example an
// invalid or unreadable TLS root CA bundle) to request time without changing
// the non-error signatures of New / NewClient. This fails closed: a
// misconfigured client never silently falls back to system trust.
type errorRoundTripper struct {
	err error
}

func (e errorRoundTripper) RoundTrip(*nethttp.Request) (*nethttp.Response, error) {
	return nil, e.err
}
