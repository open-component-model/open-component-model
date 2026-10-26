package retry

import (
	"context"
	"errors"
	"io"
	"net"
	"net/url"
	"strings"
	"syscall"
)

// BodyReadError marks an error returned while reading an HTTP response body,
// so an io.ErrUnexpectedEOF from a connection cut mid-body can be told apart
// from one produced by a truncated or corrupt local file or archive.
type BodyReadError struct {
	Err error
}

func (e *BodyReadError) Error() string { return e.Err.Error() }
func (e *BodyReadError) Unwrap() error { return e.Err }

// http2TransientMessages are the messages of net/http's HTTP/2 errors that mean
// the connection or stream died underneath an otherwise valid request. The
// error types are unexported (net/http/internal/http2), so they can only be
// matched by message; matching the text also covers callers that flattened the
// error chain with %v.
var http2TransientMessages = []string{
	"http2: client connection lost",                               // health-check ping timed out
	"http2: server sent GOAWAY and closed the connection",         // connection closed after GOAWAY
	"http2: Transport received Server's graceful shutdown GOAWAY", // stream above LastStreamID
	"http2: client conn is closed",
	"; INTERNAL_ERROR",    // StreamError: peer aborted the stream
	"; REFUSED_STREAM",    // StreamError: peer did not process the stream
	"; ENHANCE_YOUR_CALM", // StreamError: peer is shedding load
}

// IsTransientError reports whether err is a network failure that is worth
// retrying with a fresh connection: a connection reset or broken pipe, a
// message truncated on the wire, a network timeout, or an HTTP/2
// connection/stream failure. Cancellation and exceeded context deadlines are
// never transient: a retry would start with the same exhausted budget, so a
// slow server would only time out again.
//
// io.ErrUnexpectedEOF counts only when it came out of an HTTP round trip
// (*url.Error) or a response body read (*BodyReadError); from a local file or
// archive it means corrupt content, which no retry fixes.
func IsTransientError(err error) bool {
	return isTransient(err, false)
}

// isTransientRoundTripError is IsTransientError for an error returned by
// http.RoundTripper.RoundTrip, which is network-originated by construction.
func isTransientRoundTripError(err error) bool {
	return isTransient(err, true)
}

func isTransient(err error, fromRoundTrip bool) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE) {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) && (fromRoundTrip || fromNetwork(err)) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	msg := err.Error()
	for _, m := range http2TransientMessages {
		if strings.Contains(msg, m) {
			return true
		}
	}
	return false
}

func fromNetwork(err error) bool {
	var urlErr *url.Error
	var bodyErr *BodyReadError
	return errors.As(err, &urlErr) || errors.As(err, &bodyErr)
}
