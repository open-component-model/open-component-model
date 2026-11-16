package blob_test

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

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
)

// unknownSizeBlob hides the size and digest of its content, like a streamed download.
type unknownSizeBlob struct{ content string }

func (b unknownSizeBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(b.content)), nil
}

func TestArtifactBlob_Spool(t *testing.T) {
	const content = "streamed content of unknown size"
	sum := sha256.Sum256([]byte(content))
	contentDigest := hex.EncodeToString(sum[:])

	resource := func(digestValue string) *descriptor.Resource {
		r := &descriptor.Resource{Access: &v2.LocalBlob{MediaType: "application/octet-stream"}}
		if digestValue != "" {
			r.Digest = &descriptor.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: "genericBlobDigest/v1", Value: digestValue}
		}
		return r
	}

	t.Run("records size and digest and removes the file", func(t *testing.T) {
		r := require.New(t)
		dir := t.TempDir()
		ab, err := ociblob.NewArtifactBlob(resource(""), unknownSizeBlob{content})
		r.NoError(err)
		r.Equal(blob.SizeUnknown, ab.Size())

		spooled, remove, err := ab.Spool(dir)
		r.NoError(err)
		r.Equal(int64(len(content)), spooled.Size())
		dig, ok := spooled.Digest()
		r.True(ok)
		r.Equal("sha256:"+contentDigest, dig)

		for range 2 {
			rc, err := spooled.ReadCloser()
			r.NoError(err)
			data, err := io.ReadAll(rc)
			r.NoError(err)
			r.NoError(rc.Close())
			r.Equal(content, string(data))
		}

		r.NoError(remove())
		entries, err := os.ReadDir(dir)
		r.NoError(err)
		r.Empty(entries)
	})

	t.Run("rejects content that does not match the resource digest", func(t *testing.T) {
		r := require.New(t)
		dir := t.TempDir()
		ab, err := ociblob.NewArtifactBlob(resource(strings.Repeat("0", 64)), direct.NewFromBytes([]byte(content)))
		r.NoError(err)

		_, _, err = ab.Spool(dir)
		r.ErrorContains(err, "digest mismatch")
		entries, err := os.ReadDir(dir)
		r.NoError(err)
		r.Empty(entries, "a rejected spool file must not be left behind")
	})

	t.Run("creates a missing directory", func(t *testing.T) {
		r := require.New(t)
		dir := filepath.Join(t.TempDir(), "not", "yet", "there")
		ab, err := ociblob.NewArtifactBlob(resource(""), unknownSizeBlob{content})
		r.NoError(err)

		spooled, remove, err := ab.Spool(dir)
		r.NoError(err)
		r.Equal(int64(len(content)), spooled.Size())
		r.DirExists(dir)
		r.NoError(remove())
	})

	t.Run("fails when the directory cannot be used", func(t *testing.T) {
		r := require.New(t)
		notADir := filepath.Join(t.TempDir(), "file")
		r.NoError(os.WriteFile(notADir, nil, 0o600))
		ab, err := ociblob.NewArtifactBlob(resource(""), unknownSizeBlob{content})
		r.NoError(err)

		_, _, err = ab.Spool(notADir)
		r.ErrorContains(err, "failed to create spool directory")
	})

	t.Run("verifies a SHA-512 resource digest with SHA-512", func(t *testing.T) {
		r := require.New(t)
		sum := sha512.Sum512([]byte(content))
		res := resource("")
		res.Digest = &descriptor.Digest{HashAlgorithm: "SHA-512", NormalisationAlgorithm: "genericBlobDigest/v1", Value: hex.EncodeToString(sum[:])}
		ab, err := ociblob.NewArtifactBlob(res, unknownSizeBlob{content})
		r.NoError(err)

		spooled, remove, err := ab.Spool(t.TempDir())
		r.NoError(err)
		dig, ok := spooled.Digest()
		r.True(ok)
		r.Equal("sha512:"+hex.EncodeToString(sum[:]), dig)
		r.NoError(remove())
	})

	t.Run("an empty blob is spooled as an empty file", func(t *testing.T) {
		r := require.New(t)
		ab, err := ociblob.NewArtifactBlob(resource(""), unknownSizeBlob{""})
		r.NoError(err)

		spooled, remove, err := ab.Spool(t.TempDir())
		r.NoError(err)
		r.Zero(spooled.Size())
		empty := sha256.Sum256(nil)
		dig, _ := spooled.Digest()
		r.Equal("sha256:"+hex.EncodeToString(empty[:]), dig)
		r.NoError(remove())
	})

	for name, source := range map[string]failingBlob{
		"a read error":  {content: content, readErr: errors.New("connection reset")},
		"a close error": {content: content, closeErr: errors.New("checksum mismatch at end of body")},
	} {
		t.Run("removes the file after "+name+" of the source", func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			ab, err := ociblob.NewArtifactBlob(resource(""), source)
			r.NoError(err)

			_, _, err = ab.Spool(dir)
			r.ErrorContains(err, "failed to spool blob to file")
			entries, err := os.ReadDir(dir)
			r.NoError(err)
			r.Empty(entries)
		})
	}
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
