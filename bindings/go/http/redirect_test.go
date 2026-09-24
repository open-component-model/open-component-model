package http_test

import (
	"errors"
	"fmt"
	nethttp "net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	"ocm.software/open-component-model/bindings/go/http/internal/retry"
)

func redirectRequest(t *testing.T, scheme string) *nethttp.Request {
	t.Helper()
	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, scheme+"://example.com/path", nil)
	require.NoError(t, err)
	return req
}

func TestWithHTTPSDowngradeProtection_Redirects(t *testing.T) {
	for _, tt := range []struct {
		name    string
		target  string
		history []string
		blocked bool
	}{
		{name: "no history", target: "http"},
		{name: "plain HTTP", target: "http", history: []string{"http", "http"}},
		{name: "upgrade", target: "https", history: []string{"http"}},
		{name: "HTTPS", target: "https", history: []string{"https", "https"}},
		{name: "downgrade", target: "http", history: []string{"https"}, blocked: true},
		{name: "downgrade after upgrade", target: "http", history: []string{"http", "https"}, blocked: true},
		{name: "HTTPS earlier in history", target: "http", history: []string{"http", "https", "http"}, blocked: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var via []*nethttp.Request
			for _, scheme := range tt.history {
				via = append(via, redirectRequest(t, scheme))
			}
			client := ocmhttp.WithHTTPSDowngradeProtection(&nethttp.Client{})
			err := client.CheckRedirect(redirectRequest(t, tt.target), via)
			if tt.blocked {
				r.EqualError(err, "redirect from HTTPS to HTTP is not allowed")
			} else {
				r.NoError(err)
			}
		})
	}
}

func TestWithHTTPSDowngradeProtection_ClientOwnership(t *testing.T) {
	r := require.New(t)
	jar, err := cookiejar.New(nil)
	r.NoError(err)
	transport := &nethttp.Transport{}
	original := &nethttp.Client{Transport: transport, Jar: jar, Timeout: 42 * time.Second}
	protected := ocmhttp.WithHTTPSDowngradeProtection(original)
	r.NotSame(original, protected)
	r.Same(transport, protected.Transport)
	r.Same(jar, protected.Jar)
	r.Equal(original.Timeout, protected.Timeout)
	r.Nil(original.CheckRedirect)
	protected.Timeout = time.Second
	r.Equal(42*time.Second, original.Timeout)
}

func TestWithHTTPSDowngradeProtection_CustomCallback(t *testing.T) {
	for _, callbackErr := range []error{nil, errors.New("custom policy"), nethttp.ErrUseLastResponse} {
		t.Run(fmt.Sprint(callbackErr), func(t *testing.T) {
			r := require.New(t)
			calls := 0
			req := redirectRequest(t, "https")
			via := make([]*nethttp.Request, 11)
			for i := range via {
				via[i] = redirectRequest(t, "https")
			}
			original := &nethttp.Client{CheckRedirect: func(got *nethttp.Request, history []*nethttp.Request) error {
				calls++
				r.Same(req, got)
				r.Same(via[0], history[0])
				r.Len(history, len(via))
				return callbackErr
			}}
			protected := ocmhttp.WithHTTPSDowngradeProtection(original)
			// A custom policy replaces the default limit, even beyond ten redirects.
			err := protected.CheckRedirect(req, via)
			if callbackErr == nil {
				r.NoError(err)
			} else {
				r.ErrorIs(err, callbackErr)
				r.Same(callbackErr, err)
			}
			r.Equal(1, calls)

			req.URL.Scheme = "http"
			r.EqualError(protected.CheckRedirect(req, via), "redirect from HTTPS to HTTP is not allowed")
			r.Equal(1, calls, "downgrades must be rejected before the custom callback")
			_ = original.CheckRedirect(req, via)
			r.Equal(2, calls, "the original callback must remain unchanged")
		})
	}
}

func TestWithHTTPSDowngradeProtection_DefaultLimit(t *testing.T) {
	for _, count := range []int{0, 1, 9, 10, 11} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			r := require.New(t)
			via := make([]*nethttp.Request, count)
			for i := range via {
				via[i] = redirectRequest(t, "https")
			}
			client := ocmhttp.WithHTTPSDowngradeProtection(&nethttp.Client{})
			err := client.CheckRedirect(redirectRequest(t, "https"), via)
			if count >= 10 {
				r.EqualError(err, "stopped after 10 redirects")
			} else {
				r.NoError(err)
			}
		})
	}
}

func TestWithHTTPSDowngradeProtection_NilClient(t *testing.T) {
	r := require.New(t)
	client := ocmhttp.WithHTTPSDowngradeProtection(nil)
	r.NotNil(client)
	r.IsType(&retry.Transport{}, client.Transport)
	r.Zero(client.Timeout)
	r.EqualError(client.CheckRedirect(redirectRequest(t, "http"), []*nethttp.Request{redirectRequest(t, "https")}), "redirect from HTTPS to HTTP is not allowed")
	r.Nil(ocmhttp.New().CheckRedirect)
	r.Nil(ocmhttp.NewClient(nil).CheckRedirect)
}

func TestWithHTTPSDowngradeProtection_BlocksBeforeSending(t *testing.T) {
	r := require.New(t)
	var requests int
	target := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, req *nethttp.Request) {
		requests++
		w.WriteHeader(nethttp.StatusOK)
	}))
	t.Cleanup(target.Close)
	source := httptest.NewTLSServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, req *nethttp.Request) {
		nethttp.Redirect(w, req, target.URL, nethttp.StatusFound)
	}))
	t.Cleanup(source.Close)

	client := ocmhttp.WithHTTPSDowngradeProtection(source.Client())
	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodGet, source.URL, nil)
	r.NoError(err)
	resp, err := client.Do(req)
	r.ErrorContains(err, "redirect from HTTPS to HTTP is not allowed")
	if resp != nil {
		r.NoError(resp.Body.Close())
	}
	r.Zero(requests)
}
