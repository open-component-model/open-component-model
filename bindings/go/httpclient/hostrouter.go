package httpclient

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// hostRouter dispatches each request to a per-host RoundTripper when the
// request URL's host matches an entry in hosts; otherwise it falls back to
// globalRT. When the matched entry has a positive overall timeout, the
// request context is given a fresh deadline before being dispatched.
//
// Applying the timeout here (rather than via http.Client.Timeout) lets a
// per-host timeout exceed the global one — http.Client.Timeout would
// otherwise cap every request at the global value.
//
// Map keys may be either "host" or "host:port". pick checks the full host
// first so an entry with an explicit port wins over the bare hostname.
type hostRouter struct {
	globalRT      http.RoundTripper
	globalTimeout time.Duration

	hosts        map[string]http.RoundTripper
	hostTimeouts map[string]time.Duration
}

func (r *hostRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	rt, timeout := r.pick(req.URL)
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		defer cancel()
		req = req.Clone(ctx)
	}
	return rt.RoundTrip(req)
}

func (r *hostRouter) pick(u *url.URL) (http.RoundTripper, time.Duration) {
	if rt, ok := r.hosts[u.Host]; ok {
		return rt, r.hostTimeouts[u.Host]
	}
	if name := u.Hostname(); name != u.Host {
		if rt, ok := r.hosts[name]; ok {
			return rt, r.hostTimeouts[name]
		}
	}
	return r.globalRT, r.globalTimeout
}
