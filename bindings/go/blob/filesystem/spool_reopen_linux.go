package filesystem

import (
	"os"
	"strconv"
)

// reopenSpoolFile opens the spool file again and returns a handle with its own read
// position. The file has no name any more, so it is reached through the entry procfs keeps
// for the open handle; that needs /proc to be mounted.
func reopenSpoolFile(file *os.File) (reopened *os.File, err error) {
	conn, err := file.SyscallConn()
	if err != nil {
		return nil, err
	}
	// Control keeps the descriptor from being closed while it is looked up by number.
	if controlErr := conn.Control(func(fd uintptr) {
		reopened, err = os.Open("/proc/self/fd/" + strconv.FormatUint(uint64(fd), 10))
	}); controlErr != nil {
		return nil, controlErr
	}
	return reopened, err
}
