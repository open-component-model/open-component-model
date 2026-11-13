//go:build !windows

package retry

import (
	"errors"
	"syscall"
)

// isConnectionError reports whether err is the peer tearing the connection
// down: a reset, an aborted connection, or a write to a closed one.
func isConnectionError(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE)
}
