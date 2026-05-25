package httpclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingRT captures the request it received. It lets pick tests assert
// which inner RT was selected without standing up a real HTTP listener.
type recordingRT struct {
	name string
}

func (r *recordingRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Body: http.NoBody, Request: req}, nil
}

func TestHostRouter_Pick(t *testing.T) {
	global := &recordingRT{name: "global"}
	hostA := &recordingRT{name: "hostA"}
	hostAPort := &recordingRT{name: "hostA:8080"}

	r := &hostRouter{
		globalRT: global,
		hosts: map[string]http.RoundTripper{
			"a.example.com":      hostA,
			"a.example.com:8080": hostAPort,
		},
		hostTimeouts: map[string]time.Duration{
			"a.example.com":      0,
			"a.example.com:8080": 250 * time.Millisecond,
		},
	}

	tests := []struct {
		name        string
		rawURL      string
		wantRT      *recordingRT
		wantTimeout time.Duration
	}{
		{
			name:   "bare hostname matches bare key",
			rawURL: "https://a.example.com/foo",
			wantRT: hostA,
		},
		{
			name:        "host:port matches the explicit key first",
			rawURL:      "https://a.example.com:8080/foo",
			wantRT:      hostAPort,
			wantTimeout: 250 * time.Millisecond,
		},
		{
			name:   "host:port falls back to bare hostname when no explicit key",
			rawURL: "https://a.example.com:9999/foo",
			wantRT: hostA,
		},
		{
			name:   "unknown host falls back to global",
			rawURL: "https://other.example.com/foo",
			wantRT: global,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			u, err := url.Parse(tc.rawURL)
			require.NoError(t, err)

			rt, timeout := r.pick(u)
			assert.Same(t, tc.wantRT, rt)
			assert.Equal(t, tc.wantTimeout, timeout)
		})
	}
}

func TestHostRouter_RoundTrip_AppliesPerHostDeadline(t *testing.T) {
	var observedDeadline time.Time
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		d, ok := req.Context().Deadline()
		require.True(t, ok, "expected request context to carry a per-host deadline")
		observedDeadline = d
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})

	r := &hostRouter{
		globalRT: &recordingRT{name: "global"},
		hosts: map[string]http.RoundTripper{
			"slow.example.com": inner,
		},
		hostTimeouts: map[string]time.Duration{
			"slow.example.com": 50 * time.Millisecond,
		},
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://slow.example.com/foo", nil)
	require.NoError(t, err)

	resp, err := r.RoundTrip(req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	require.False(t, observedDeadline.IsZero())
	assert.LessOrEqual(t, time.Until(observedDeadline), 50*time.Millisecond)
}

func TestHostRouter_RoundTrip_DoesNotExtendParentDeadline(t *testing.T) {
	// context.WithTimeout(parent, big) cannot extend a parent's shorter
	// deadline. This test pins that contract: a per-host timeout larger than
	// the inherited deadline does not loosen the inherited one.
	inner := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})

	r := &hostRouter{
		globalRT: inner,
		hosts: map[string]http.RoundTripper{
			"slow.example.com": inner,
		},
		hostTimeouts: map[string]time.Duration{
			"slow.example.com": time.Hour,
		},
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://slow.example.com/foo", nil)
	require.NoError(t, err)

	_, err = r.RoundTrip(req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

// roundTripFunc is the standard http.RoundTripper adapter for closures.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
