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

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
