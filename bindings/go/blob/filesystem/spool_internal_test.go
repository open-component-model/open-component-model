package filesystem

import (
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type stringBlob string

func (b stringBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(b))), nil
}

// Where the operating system cannot open the spool file again, all readers share its one
// handle. They must behave like files of their own all the same: independent positions,
// rewindable, and closing one leaves the handle open for the others.
func TestSpool_SharedHandleReaders(t *testing.T) {
	r := require.New(t)
	const content = "0123456789abcdefghij"
	spooled, err := Spool(stringBlob(content), t.TempDir(), "")
	r.NoError(err)
	files, ok := spooled.fileSystem.(spoolFS)
	r.True(ok)

	first, err := files.openShared()
	r.NoError(err)
	half := make([]byte, len(content)/2)
	_, err = io.ReadFull(first, half)
	r.NoError(err)

	second, err := files.openShared()
	r.NoError(err)
	all, err := io.ReadAll(second)
	r.NoError(err)
	r.Equal(content, string(all))
	r.NoError(second.Close())

	rest, err := io.ReadAll(first)
	r.NoError(err)
	r.Equal(content, string(half)+string(rest))

	seeker, ok := first.(io.Seeker)
	r.True(ok, "the reader cannot seek")
	_, err = seeker.Seek(0, io.SeekStart)
	r.NoError(err)
	all, err = io.ReadAll(first)
	r.NoError(err)
	r.Equal(content, string(all))
	info, err := first.Stat()
	r.NoError(err)
	r.Equal(int64(len(content)), info.Size())
	r.NoError(first.Close())
	r.NoError(spooled.Remove())
}
