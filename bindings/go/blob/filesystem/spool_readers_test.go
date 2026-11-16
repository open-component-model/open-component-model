package filesystem_test

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// Each reader of a spooled blob has its own position: reading or closing one must not move
// or invalidate another, whether the operating system can open the spool file again or all
// readers share its one handle.
func TestSpool_ReadersAreIndependent(t *testing.T) {
	r := require.New(t)
	const content = "0123456789abcdefghij"
	spooled, err := filesystem.Spool(streamedBlob{content}, t.TempDir(), "")
	r.NoError(err)

	first, err := spooled.ReadCloser()
	r.NoError(err)
	half := make([]byte, len(content)/2)
	_, err = io.ReadFull(first, half)
	r.NoError(err)

	second, err := spooled.ReadCloser()
	r.NoError(err)
	all, err := io.ReadAll(second)
	r.NoError(err)
	r.Equal(content, string(all), "a new reader starts at the beginning")
	r.NoError(second.Close())

	rest, err := io.ReadAll(first)
	r.NoError(err)
	r.Equal(content, string(half)+string(rest), "closing one reader leaves the others usable")
	r.NoError(first.Close())

	third, err := spooled.ReadCloser()
	r.NoError(err)
	all, err = io.ReadAll(third)
	r.NoError(err)
	r.Equal(content, string(all))
	r.NoError(third.Close())
	r.NoError(spooled.Close())
}

// An upload that fails half way is sent again from the start. HTTP clients do that only for
// a request body they can rewind, so a reader of a spooled blob has to seek.
func TestSpool_ReaderCanBeRewound(t *testing.T) {
	r := require.New(t)
	const content = "0123456789abcdefghij"
	spooled, err := filesystem.Spool(streamedBlob{content}, t.TempDir(), "")
	r.NoError(err)

	reader, err := spooled.ReadCloser()
	r.NoError(err)
	_, err = io.CopyN(io.Discard, reader, int64(len(content)/2))
	r.NoError(err)

	seeker, ok := reader.(io.Seeker)
	r.True(ok, "the reader cannot seek")
	_, err = seeker.Seek(0, io.SeekStart)
	r.NoError(err)
	all, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(content, string(all))
	r.NoError(reader.Close())
	r.NoError(spooled.Close())
}

const (
	spoolChildDirEnv   = "OCM_TEST_SPOOL_CHILD_DIR"
	spoolChildIsReady  = "SPOOLED"
	spoolChildLifetime = time.Minute
)

// A process that is killed cannot clean up after itself, so the operating system has to
// free the spool file. The test spools in a child process, kills it and looks for leftovers.
func TestSpool_KilledProcessLeavesNoFile(t *testing.T) {
	if dir := os.Getenv(spoolChildDirEnv); dir != "" {
		spoolAndWaitToBeKilled(t, dir)
		return
	}
	r := require.New(t)
	dir := t.TempDir()

	// The child is killed explicitly: a context that ends with the test would race that.
	child := exec.CommandContext(context.WithoutCancel(t.Context()), os.Args[0], "-test.run=^TestSpool_KilledProcessLeavesNoFile$")
	child.Env = append(os.Environ(), spoolChildDirEnv+"="+dir)
	stdout, err := child.StdoutPipe()
	r.NoError(err)
	r.NoError(child.Start())
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})

	ready := false
	for scanner := bufio.NewScanner(stdout); scanner.Scan(); {
		if scanner.Text() == spoolChildIsReady {
			ready = true
			break
		}
	}
	r.True(ready, "the child process did not spool a blob")

	r.NoError(child.Process.Kill())
	_ = child.Wait()
	// Windows removes the file once it has closed the handles of the dead process.
	r.Eventually(func() bool {
		entries, err := os.ReadDir(dir)
		return err == nil && len(entries) == 0
	}, 10*time.Second, 20*time.Millisecond, "the killed process left its spool file behind")
}

// spoolAndWaitToBeKilled is the child process: it holds a spooled blob and an open reader of
// it until the parent kills it.
func spoolAndWaitToBeKilled(t *testing.T, dir string) {
	t.Helper()
	spooled, err := filesystem.Spool(streamedBlob{strings.Repeat("x", 1<<20)}, dir, "")
	require.NoError(t, err)
	reader, err := spooled.ReadCloser()
	require.NoError(t, err)

	_, err = io.WriteString(os.Stdout, spoolChildIsReady+"\n")
	require.NoError(t, err)
	time.Sleep(spoolChildLifetime)
	// The handles must still be open when the process is killed.
	runtime.KeepAlive(spooled)
	runtime.KeepAlive(reader)
}
