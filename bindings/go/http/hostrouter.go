package http

import (
	"context"
	"io"
	nethttp "net/http"
	"net/url"
	"time"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// hostRouter dispatches each request to a per-host [nethttp.RoundTripper] when the
// request URL's host matches an entry in hosts; otherwise it falls back to
// globalRT. When the matched entry has a positive overall timeout, the
// request context is given a fresh deadline before being dispatched.
//
// Applying the timeout here (rather than via http.Client.Timeout) lets a
// per-host timeout exceed the global one — http.Client.Timeout would
// otherwise cap every request at the global value.
//
// Map keys may be either "host" or "host:port", resolved in the order given by
// [httpv1alpha1.HostKeys], so an entry with an explicit port wins over the bare
// hostname.
//
// The per-host timeout deadline covers the entire request lifecycle including
// reading the response body: the cancel function is deferred until the response
// body is closed via cancelOnCloseBody.
type hostRouter struct {
	globalRT      nethttp.RoundTripper
	globalTimeout time.Duration

	hosts        map[string]nethttp.RoundTripper
	hostTimeouts map[string]time.Duration
}

func (r *hostRouter) RoundTrip(req *nethttp.Request) (*nethttp.Response, error) {
	rt, timeout := r.pick(req.URL)
	if timeout <= 0 {
		return rt.RoundTrip(req)
	}
	ctx, cancel := context.WithTimeout(req.Context(), timeout)
	resp, err := rt.RoundTrip(req.Clone(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// cancelOnCloseBody defers cancellation of the per-host timeout context until
// the response body is closed, so the deadline covers the full response body
// read rather than ending as soon as headers are received.
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

func (r *hostRouter) pick(u *url.URL) (nethttp.RoundTripper, time.Duration) {
	for _, key := range httpv1alpha1.HostKeys(u.Host) {
		if rt, ok := r.hosts[key]; ok {
			return rt, r.hostTimeouts[key]
		}
	}
	return r.globalRT, r.globalTimeout
}
