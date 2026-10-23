package retry

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
)

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
// truncated message, a network timeout, or an HTTP/2 connection/stream
// failure. Cancellation is never transient.
func IsTransientError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.ErrUnexpectedEOF) {
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
