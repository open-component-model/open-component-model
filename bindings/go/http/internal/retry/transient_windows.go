package retry

import (
	"errors"
	"syscall"
)

// isConnectionError reports whether err is the peer tearing the connection
// down: a reset, an aborted connection, or a write to a closed one.
//
// Winsock reports those as WSAE* codes. syscall.ECONNRESET and friends exist on
// Windows only as synthetic APPLICATION_ERROR values that no socket ever
// returns, so they have to be matched in addition, not instead: they still show
// up in errors built by Go code rather than by the socket layer.
func isConnectionError(err error) bool {
	return errors.Is(err, syscall.WSAECONNRESET) ||
		errors.Is(err, syscall.WSAECONNABORTED) ||
		errors.Is(err, syscall.ERROR_NETNAME_DELETED) ||
		errors.Is(err, syscall.ERROR_BROKEN_PIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE)
}
