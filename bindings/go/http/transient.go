package http

import "ocm.software/open-component-model/bindings/go/http/internal/retry"

// IsTransientError reports whether err is a network failure worth retrying on
// a fresh connection: connection reset, broken pipe, unexpected EOF on the
// wire, network timeout, or HTTP/2 connection/stream loss. Exceeded context
// deadlines (including http.Client.Timeout) and unexpected EOF from local data
// are not transient. It is the classification the retry transport of New uses
// for requests; callers use it to retry whole operations that fail after a
// response started streaming, which no transport can replay.
func IsTransientError(err error) bool {
	return retry.IsTransientError(err)
}
