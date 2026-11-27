package filesystem_test

import (
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// streamedBlob hides the size and digest of its content, like a streamed download.
type streamedBlob struct{ content string }

func (b streamedBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(b.content)), nil
}

// failingBlob yields content and then fails the read or the close of its reader, like a
// download that breaks off or that verifies its content at the end of the body.
type failingBlob struct {
	content           string
	readErr, closeErr error
}

func (b failingBlob) ReadCloser() (io.ReadCloser, error) {
	return &failingReader{Reader: strings.NewReader(b.content), blob: b}, nil
}

type failingReader struct {
	io.Reader
	blob failingBlob
}

func (r *failingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF && r.blob.readErr != nil {
		return n, r.blob.readErr
	}
	return n, err
}

func (r *failingReader) Close() error { return r.blob.closeErr }

func TestSpool(t *testing.T) {
	const content = "streamed content of unknown size"

	t.Run("records size and digest and frees the file when removed", func(t *testing.T) {
		r := require.New(t)
		dir := t.TempDir()
		sum := sha256.Sum256([]byte(content))

		spooled, err := filesystem.Spool(streamedBlob{content}, dir, "")
		r.NoError(err)
		r.Equal(int64(len(content)), spooled.Size())
		dig, ok := spooled.Digest()
		r.True(ok)
		r.Equal("sha256:"+hex.EncodeToString(sum[:]), dig)

		for range 2 {
			rc, err := spooled.ReadCloser()
			r.NoError(err)
			data, err := io.ReadAll(rc)
			r.NoError(err)
			r.NoError(rc.Close())
			r.Equal(content, string(data))
		}

		r.NoError(spooled.Remove())
		entries, err := os.ReadDir(dir)
		r.NoError(err)
		r.Empty(entries)
	})

	t.Run("cannot be written or read once it is removed", func(t *testing.T) {
		r := require.New(t)
		spooled, err := filesystem.Spool(streamedBlob{content}, t.TempDir(), "")
		r.NoError(err)
		// Its digest was recorded while it was written, so the content must not change.
		_, err = spooled.WriteCloser()
		r.Error(err)

		r.NoError(spooled.Remove())
		_, err = spooled.ReadCloser()
		r.Error(err)
	})

	t.Run("computes the digest with the requested algorithm", func(t *testing.T) {
		r := require.New(t)
		sum := sha512.Sum512([]byte(content))

		spooled, err := filesystem.Spool(streamedBlob{content}, t.TempDir(), digest.SHA512)
		r.NoError(err)
		dig, _ := spooled.Digest()
		r.Equal("sha512:"+hex.EncodeToString(sum[:]), dig)
		r.NoError(spooled.Remove())
	})

	t.Run("rejects an algorithm it cannot compute", func(t *testing.T) {
		_, err := filesystem.Spool(streamedBlob{content}, t.TempDir(), "md5")
		require.ErrorContains(t, err, "not available")
	})

	t.Run("an empty blob is spooled as an empty file", func(t *testing.T) {
		r := require.New(t)
		empty := sha256.Sum256(nil)

		spooled, err := filesystem.Spool(streamedBlob{""}, t.TempDir(), "")
		r.NoError(err)
		r.Zero(spooled.Size())
		dig, _ := spooled.Digest()
		r.Equal("sha256:"+hex.EncodeToString(empty[:]), dig)
		rc, err := spooled.ReadCloser()
		r.NoError(err)
		data, err := io.ReadAll(rc)
		r.NoError(err)
		r.Empty(data)
		r.NoError(rc.Close())
		r.NoError(spooled.Remove())
	})

	t.Run("creates a missing directory", func(t *testing.T) {
		r := require.New(t)
		dir := filepath.Join(t.TempDir(), "not", "yet", "there")

		spooled, err := filesystem.Spool(streamedBlob{content}, dir, "")
		r.NoError(err)
		r.DirExists(dir)
		r.NoError(spooled.Remove())
	})

	t.Run("fails when the directory cannot be used", func(t *testing.T) {
		r := require.New(t)
		notADir := filepath.Join(t.TempDir(), "file")
		r.NoError(os.WriteFile(notADir, nil, 0o600))

		_, err := filesystem.Spool(streamedBlob{content}, notADir, "")
		r.ErrorContains(err, "failed to create spool directory")
	})

	sourceErr := errors.New("connection reset")
	for name, source := range map[string]failingBlob{
		"a read error":  {content: content, readErr: sourceErr},
		"a close error": {content: content, closeErr: sourceErr},
	} {
		t.Run("leaves no file after "+name+" of the source", func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()

			_, err := filesystem.Spool(source, dir, "")
			r.ErrorIs(err, sourceErr)
			entries, err := os.ReadDir(dir)
			r.NoError(err)
			r.Empty(entries)
		})
	}
}
