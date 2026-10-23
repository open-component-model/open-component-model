package http

import (
	"bytes"
	"errors"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRewindableBody_OnlyLatestAttemptReads(t *testing.T) {
	r := require.New(t)
	owner := &closeCounter{ReadSeeker: strings.NewReader("0123456789")}
	_, err := owner.Seek(2, io.SeekStart)
	r.NoError(err)
	src := &rewindableBody{rs: owner, start: 2, owner: owner}

	first := src.firstAttempt()
	buf := make([]byte, 3)
	n, err := first.Read(buf)
	r.NoError(err)
	r.Equal("234", string(buf[:n]))

	second, err := src.nextAttempt()
	r.NoError(err)
	_, err = first.Read(buf)
	r.ErrorIs(err, errStaleBody, "a superseded attempt must not read the shared source")
	r.NoError(first.Close(), "closing a superseded attempt is a no-op")

	all, err := io.ReadAll(second)
	r.NoError(err)
	r.Equal("23456789", string(all), "a replay restarts at the original offset, not at 0")

	r.NoError(second.Close())
	r.Zero(owner.closed.Load(), "the original body stays open until the round trip returned")
	src.roundTripDone()
	r.Equal(int32(1), owner.closed.Load())

	_, err = src.nextAttempt()
	r.ErrorIs(err, errStaleBody, "no replay after the original body was released")
	_, err = second.Read(buf)
	r.ErrorIs(err, errStaleBody)
}

func TestRewindableBody_ReleasesWhenTransportClosesAfterReturn(t *testing.T) {
	r := require.New(t)
	owner := &closeCounter{ReadSeeker: strings.NewReader("x")}
	src := &rewindableBody{rs: owner, owner: owner}
	body := src.firstAttempt()

	// net/http may close the body asynchronously after RoundTrip returned.
	src.roundTripDone()
	r.Zero(owner.closed.Load())
	r.NoError(body.Close())
	r.Equal(int32(1), owner.closed.Load())
	r.NoError(body.Close())
	r.Equal(int32(1), owner.closed.Load(), "the original body is closed exactly once")
}

type failingSeeker struct{ io.Reader }

func (failingSeeker) Seek(int64, int) (int64, error) { return 0, errors.New("not seekable") }
func (failingSeeker) Close() error                   { return nil }

func TestReplayableBodyTransport_PassesThroughUnreplayableBodies(t *testing.T) {
	cases := []struct {
		name string
		body io.Reader
	}{
		{"stdlib in-memory body keeps its own GetBody", bytes.NewReader([]byte("abc"))},
		{"non-seekable body", struct{ io.Reader }{strings.NewReader("abc")}},
		{"seeker that cannot report its position", failingSeeker{strings.NewReader("abc")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodPut, "http://example.invalid", tc.body)
			r.NoError(err)
			var seen *nethttp.Request
			rt := &replayableBodyTransport{base: roundTripFunc(func(got *nethttp.Request) (*nethttp.Response, error) {
				seen = got
				return &nethttp.Response{StatusCode: nethttp.StatusOK, Body: nethttp.NoBody}, nil
			})}
			_, err = rt.RoundTrip(req)
			r.NoError(err)
			r.Same(req, seen, "the request must be forwarded unchanged")
		})
	}
}

// TestNew_RetriesSeekableBodyOn5xx covers the retry transport rewinding a
// file-backed body: before, a PUT whose body was not an in-memory reader was
// never retried on a 5xx.
func TestNew_RetriesSeekableBodyOn5xx(t *testing.T) {
	r := require.New(t)
	data := []byte("prefix-to-skip|blob-content")
	const offset = int64(len("prefix-to-skip|"))
	path := filepath.Join(t.TempDir(), "blob")
	r.NoError(os.WriteFile(path, data, 0o600))

	var calls atomic.Int32
	var received [][]byte
	srv := httptest.NewServer(nethttp.HandlerFunc(func(w nethttp.ResponseWriter, req *nethttp.Request) {
		b, _ := io.ReadAll(req.Body)
		received = append(received, b)
		if calls.Add(1) == 1 {
			w.WriteHeader(nethttp.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(nethttp.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	f, err := os.Open(path)
	r.NoError(err)
	_, err = f.Seek(offset, io.SeekStart)
	r.NoError(err)

	req, err := nethttp.NewRequestWithContext(t.Context(), nethttp.MethodPut, srv.URL, f)
	r.NoError(err)
	req.ContentLength = int64(len(data)) - offset
	resp, err := New().Do(req)
	r.NoError(err)
	r.NoError(resp.Body.Close())
	r.Equal(nethttp.StatusCreated, resp.StatusCode)
	r.Equal([][]byte{[]byte("blob-content"), []byte("blob-content")}, received)

	_, err = f.Read(make([]byte, 1))
	r.ErrorIs(err, os.ErrClosed, "the transport still owns and closes req.Body")
}
