package blob_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
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
}
