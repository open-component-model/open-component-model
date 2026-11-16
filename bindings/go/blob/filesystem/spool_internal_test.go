package filesystem

import (
	"io"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type stringBlob string

func (b stringBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b))), nil
}

// The file system of a spooled blob holds exactly its one file: nothing else can be opened,
// inspected or removed through it.
func TestSpool_FileSystemHoldsOnlyTheSpoolFile(t *testing.T) {
	r := require.New(t)
	const content = "0123456789abcdefghij"
	spooled, err := Spool(stringBlob(content), t.TempDir(), "")
	r.NoError(err)
	files, ok := spooled.fileSystem.(spoolFS)
	r.True(ok)

	_, err = files.Open("other")
	r.ErrorIs(err, fs.ErrNotExist)
	_, err = files.Stat("other")
	r.ErrorIs(err, fs.ErrNotExist)
	r.ErrorIs(files.Remove("other"), fs.ErrNotExist)

	file, err := files.Open(files.name)
	r.NoError(err)
	info, err := file.Stat()
	r.NoError(err)
	r.Equal(int64(len(content)), info.Size())
	r.NoError(file.Close())
	r.NoError(spooled.Remove())
}
