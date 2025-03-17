//go:build unix

package filesystem_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func TestCopyBlobToOSPath_NamedPipe_Blocking(t *testing.T) {
	r := require.New(t)
	tempDir := t.TempDir()
	pipePath := filepath.Join(tempDir, "pipe")
	r.NoError(unix.Mkfifo(pipePath, 0666))

	testData := []byte("test data")

	fsys, err := filesystem.NewFS(tempDir, os.O_RDWR)
	r.NoError(err)
	filePath := "testfile.txt"
	b := filesystem.NewFileBlob(fsys, filePath)
	writer, err := b.WriteCloser()
	r.NoError(err)
	_, err = writer.Write(testData)
	r.NoError(err)
	r.NoError(writer.Close())

	data := make(chan []byte, 1)
	defer close(data)

	go func() {
		f, err := os.OpenFile(pipePath, os.O_RDONLY, os.ModeNamedPipe)
		r.NoError(err)
		defer func() {
			r.NoError(f.Close())
		}()
		all, err := io.ReadAll(f)
		r.NoError(err)
		data <- all
	}()

	r.NoError(filesystem.CopyBlobToOSPath(b, pipePath))

	r.NoError(err)

	timer := time.NewTimer(1 * time.Second)
	defer timer.Stop()

	select {
	case <-timer.C:
		r.Fail("timeout waiting for data, it never arrived in the pipe")
	case data := <-data:
		r.Equal(testData, data)
	}
}
