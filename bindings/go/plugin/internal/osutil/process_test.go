package osutil

import (
	"context"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// killHelperEnv makes the test binary double as the child process Kill is exercised against,
// which keeps the test independent of the shell utilities an operating system ships.
const killHelperEnv = "OCM_OSUTIL_KILL_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(killHelperEnv) {
	case "block":
		time.Sleep(time.Hour)
		os.Exit(1)
	case "exit":
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func helperCommand(t *testing.T, mode string) *exec.Cmd {
	t.Helper()
	// The test decides when the helper dies: a cancellable context would make os/exec kill
	// it in parallel, which is the race this helper exists to pin down.
	cmd := exec.CommandContext(context.WithoutCancel(t.Context()), os.Args[0])
	cmd.Env = append(os.Environ(), killHelperEnv+"="+mode)
	return cmd
}

func TestKill(t *testing.T) {
	t.Run("running process is terminated", func(t *testing.T) {
		r := require.New(t)
		cmd := helperCommand(t, "block")
		r.NoError(cmd.Start())
		r.NoError(Kill(cmd.Process))
		state, err := cmd.Process.Wait()
		r.NoError(err)
		r.NotEqual(0, state.ExitCode())
	})

	t.Run("exited process is already done", func(t *testing.T) {
		r := require.New(t)
		cmd := helperCommand(t, "exit")
		stdout, err := cmd.StdoutPipe()
		r.NoError(err)
		r.NoError(cmd.Start())

		// The pipe reaches EOF once the child is gone. It stays unreaped, so os.Process does
		// not know that yet and the kill reaches the operating system: unix signals the
		// zombie successfully, windows answers TerminateProcess with access denied.
		_, err = io.ReadAll(stdout)
		r.NoError(err)

		if err := Kill(cmd.Process); err != nil {
			r.ErrorIs(err, os.ErrProcessDone)
		}
		state, err := cmd.Process.Wait()
		r.NoError(err)
		// The child decided its own exit status, so nothing killed it.
		r.Equal(0, state.ExitCode())
	})
}
