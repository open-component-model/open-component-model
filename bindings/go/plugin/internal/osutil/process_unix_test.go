//go:build unix

package osutil

import (
	"math"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProcessAlive(t *testing.T) {
	t.Run("running process is alive", func(t *testing.T) {
		require.True(t, ProcessAlive(os.Getpid()))
	})

	t.Run("exited process is not alive", func(t *testing.T) {
		r := require.New(t)
		cmd := exec.CommandContext(t.Context(), "true")
		r.NoError(cmd.Run())
		// The process is reaped, so nothing holds its PID: kill(2) answers ESRCH.
		r.False(ProcessAlive(cmd.Process.Pid))
	})

	t.Run("pid outside the pid_t range is not alive", func(t *testing.T) {
		r := require.New(t)
		// kill(2) reads these as a process group or truncates them, so they never
		// answer for the process the caller asked about.
		r.False(ProcessAlive(0))
		r.False(ProcessAlive(-1))
		if math.MaxInt > math.MaxInt32 {
			r.False(ProcessAlive(math.MaxInt32 + 1))
		}
	})
}
