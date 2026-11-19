package retry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTransport_RetriesConnectionReset reproduces a registry (or LB) resetting
// a fresh connection before answering. net/http only retries that on its own
// for reused connections, so the retry transport has to.
func TestTransport_RetriesConnectionReset(t *testing.T) {
	r := require.New(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			_ = conn.(*net.TCPConn).SetLinger(0)
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	tr := NewTransport(&http.Transport{DisableKeepAlives: true})
	tr.Policy = func() Policy { return NewGenericPolicy(3, time.Millisecond, time.Millisecond) }
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	r.NoError(err)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	r.NoError(err)
	r.NoError(resp.Body.Close())
	r.Equal(http.StatusOK, resp.StatusCode)
	r.Equal(int32(2), calls.Load())
}

func TestIsTransientError(t *testing.T) {
	cases := []struct {
		err       error
		transient bool
	}{
		{nil, false},
		{errors.New("boom"), false},
		{context.Canceled, false},
		{fmt.Errorf("wrapped: %w", context.Canceled), false},
		// A retry starts with the same exhausted budget, so it would time out again.
		{fmt.Errorf("wrapped: %w", context.DeadlineExceeded), false},
		{fmt.Errorf("read: %w", syscall.ECONNRESET), true},
		{fmt.Errorf("write: %w", syscall.EPIPE), true},
		// Unexpected EOF is a network failure only when it came off the wire;
		// from a local file or archive it means corrupt content.
		{fmt.Errorf("untar: %w", io.ErrUnexpectedEOF), false},
		{fmt.Errorf("copy: %w", &BodyReadError{Err: io.ErrUnexpectedEOF}), true},
		{&url.Error{Op: "Put", URL: "https://r/v2/", Err: io.ErrUnexpectedEOF}, true},
		{&net.OpError{Op: "dial", Err: timeoutError{}}, true},
		// A peer that closes the connection without answering, and a connection that
		// net/http closed under a request body it was still writing, are network
		// failures only when they came out of a round trip.
		{&url.Error{Op: "Put", URL: "https://r/v2/", Err: io.EOF}, true},
		{fmt.Errorf("read layer: %w", io.EOF), false},
		{&url.Error{Op: "Put", URL: "https://r/v2/", Err: &net.OpError{Op: "readfrom", Net: "tcp", Err: &net.OpError{Op: "write", Net: "tcp", Err: net.ErrClosed}}}, true},
		{&net.OpError{Op: "accept", Net: "tcp", Err: net.ErrClosed}, false},
		// http.Client.Timeout surfaces without a *net.OpError and stays final.
		{&url.Error{Op: "Get", URL: "https://r/", Err: fmt.Errorf("%w (Client.Timeout exceeded)", context.DeadlineExceeded)}, false},
		// net/http's HTTP/2 errors are unexported; they are matched by message,
		// also when an intermediate layer flattened the chain with %v.
		{errors.New("Put \"https://r/v2/\": http2: client connection lost"), true},
		{errors.New("push failed: http2: server sent GOAWAY and closed the connection; LastStreamID=1, ErrCode=NO_ERROR, debug=\"\""), true},
		{errors.New("stream error: stream ID 5; INTERNAL_ERROR; received from peer"), true},
		{errors.New("stream error: stream ID 5; REFUSED_STREAM"), true},
		{errors.New("stream error: stream ID 5; PROTOCOL_ERROR"), false},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.err), func(t *testing.T) {
			require.Equal(t, tc.transient, IsTransientError(tc.err))
		})
	}
}

// TestTransport_RetriesConnectionClosedWithoutResponse reproduces a server (or
// LB) that receives a whole upload and then closes the connection without
// answering, as one that shuts down does. net/http reports a bare io.EOF.
func TestTransport_RetriesConnectionClosedWithoutResponse(t *testing.T) {
	r := require.New(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_, _ = io.Copy(io.Discard, req.Body)
		if calls.Add(1) == 1 {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				return
			}
			_ = conn.Close()
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	tr := NewTransport(&http.Transport{DisableKeepAlives: true})
	tr.Policy = func() Policy { return NewGenericPolicy(3, time.Millisecond, time.Millisecond) }
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, srv.URL, strings.NewReader("blob"))
	r.NoError(err)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	r.NoError(err)
	r.NoError(resp.Body.Close())
	r.Equal(http.StatusCreated, resp.StatusCode)
	r.EqualValues(2, calls.Load())
}

// TestTransport_RetriesConnectionClosedUnderUpload covers a connection that the
// peer cut while a request body was written. net/http reads and writes a
// connection on two goroutines; when the reading one notices first it closes
// the connection, and the write fails with net.ErrClosed instead of a reset.
// Which one notices first is a matter of timing, so the error is injected.
func TestTransport_RetriesConnectionClosedUnderUpload(t *testing.T) {
	r := require.New(t)
	closed := &net.OpError{Op: "readfrom", Net: "tcp", Err: &net.OpError{Op: "write", Net: "tcp", Err: net.ErrClosed}}
	var bodies []string
	tr := NewTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		bodies = append(bodies, string(body))
		if len(bodies) == 1 {
			return nil, closed
		}
		return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody}, nil
	}))
	tr.Policy = func() Policy { return NewGenericPolicy(3, time.Millisecond, time.Millisecond) }

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, "http://registry.example/v2/", strings.NewReader("blob"))
	r.NoError(err)
	resp, err := (&http.Client{Transport: tr}).Do(req)
	r.NoError(err)
	r.NoError(resp.Body.Close())
	r.Equal([]string{"blob", "blob"}, bodies, "the upload is sent again in full")
}

// TestIsTransientError_CanceledUpload checks that canceling a request while its
// body is written is not mistaken for the peer closing the connection: the
// cancellation closes the connection too, but must not be retried.
func TestIsTransientError_CanceledUpload(t *testing.T) {
	r := require.New(t)
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		_, _ = io.CopyN(io.Discard, req.Body, 1024)
		close(started)
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		<-started
		cancel()
	}()
	// Larger than the socket buffers, so the request is still being written.
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, srv.URL, io.LimitReader(endlessReader{}, 64<<20))
	r.NoError(err)
	req.ContentLength = 64 << 20
	resp, err := (&http.Client{Transport: &http.Transport{}}).Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	r.ErrorIs(err, context.Canceled)
	r.False(IsTransientError(err), "a canceled upload must not be transient: %v", err)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type endlessReader struct{}

func (endlessReader) Read(p []byte) (int, error) { return len(p), nil }

// TestIsTransientError_ClientTimeout checks that http.Client.Timeout firing on
// a slow server is not retried: every retry would get the same budget and fail.
func TestIsTransientError_ClientTimeout(t *testing.T) {
	r := require.New(t)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		<-release
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	client := &http.Client{Timeout: 50 * time.Millisecond}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	r.NoError(err)
	resp, err := client.Do(req)
	r.NoError(err)
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	r.Error(err)
	r.False(IsTransientError(err), "client timeout must not be transient: %v", err)
}

// TestIsTransientError_DialTimeout checks that a connect that hit
// Dialer.Timeout stays transient although its error matches
// context.DeadlineExceeded: a retry dials with a fresh budget.
//
// The dial deadline is forced to be already expired instead of racing a real
// connect against a tiny Dialer.Timeout: on Windows a loopback ConnectEx can
// complete before the deadline watcher cancels it, which hangs the request on
// a listener that never answers. The error itself still comes from net, and
// net/http does the *url.Error wrapping.
func TestIsTransientError_DialTimeout(t *testing.T) {
	r := require.New(t)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			expired, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
			defer cancel()
			return (&net.Dialer{}).DialContext(expired, network, addr)
		},
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:1", nil)
	r.NoError(err)
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if resp != nil {
		_ = resp.Body.Close()
	}
	r.ErrorIs(err, context.DeadlineExceeded)
	r.True(IsTransientError(err), "dial timeout must be transient: %v", err)
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
