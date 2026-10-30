package repository

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

const verifyContent = "content the descriptor took a digest over"

func resourceWithDigest(dig *descriptor.Digest) *descriptor.Resource {
	res := &descriptor.Resource{}
	res.Name = "test-resource"
	res.Version = "1.0.0"
	res.Digest = dig
	return res
}

func TestVerifyDownload(t *testing.T) {
	matching := &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  godigest.FromString(verifyContent).Encoded(),
	}

	t.Run("holds content to a matching digest", func(t *testing.T) {
		verified, err := VerifyDownload(t.Context(), resourceWithDigest(matching),
			inmemory.New(strings.NewReader(verifyContent)))
		require.NoError(t, err)

		rc, err := verified.ReadCloser()
		require.NoError(t, err)
		read, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		require.Equal(t, verifyContent, string(read))
	})

	t.Run("reports content that does not match", func(t *testing.T) {
		verified, err := VerifyDownload(t.Context(), resourceWithDigest(matching),
			inmemory.New(strings.NewReader("something else entirely")))
		require.NoError(t, err)

		rc, err := verified.ReadCloser()
		require.NoError(t, err)
		_, err = io.ReadAll(rc)
		require.ErrorContains(t, err, "digest mismatch")
	})

	t.Run("passes content through when there is no digest to verify against", func(t *testing.T) {
		for _, dig := range []*descriptor.Digest{
			nil,
			{},
			{HashAlgorithm: descriptor.NoDigest, NormalisationAlgorithm: descriptor.ExcludeFromSignature, Value: descriptor.NoDigest},
		} {
			content := inmemory.New(strings.NewReader(verifyContent))
			verified, err := VerifyDownload(t.Context(), resourceWithDigest(dig), content)
			require.NoError(t, err)
			require.Same(t, content, verified, "unverifiable content must be handed back untouched")
		}
	})

	t.Run("refuses content whose digest is present but unusable", func(t *testing.T) {
		for _, dig := range []*descriptor.Digest{
			{HashAlgorithm: "MD5", Value: godigest.FromString(verifyContent).Encoded()},
			{HashAlgorithm: "SHA-256", Value: "not-hex"},
			{HashAlgorithm: "SHA-256", Value: "abcd"},
			{HashAlgorithm: "SHA-256"},
			{Value: godigest.FromString(verifyContent).Encoded()},
		} {
			_, err := VerifyDownload(t.Context(), resourceWithDigest(dig),
				inmemory.New(strings.NewReader(verifyContent)))
			require.Error(t, err, "digest %+v must not pass as verifiable", dig)
		}
	})

	t.Run("reports the metadata of the content without asking for it up front", func(t *testing.T) {
		content := &lazyMetadataBlob{Blob: inmemory.New(strings.NewReader(verifyContent), inmemory.WithMediaType("application/x-tar"))}
		verified, err := VerifyDownload(t.Context(), resourceWithDigest(matching), content)
		require.NoError(t, err)
		require.Zero(t, content.asked, "a lazy source must not be asked for its metadata before it is needed")

		require.Equal(t, int64(len(verifyContent)), verified.(blob.SizeAware).Size())
		mediaType, ok := verified.(blob.MediaTypeAware).MediaType()
		require.True(t, ok)
		require.Equal(t, "application/x-tar", mediaType)
	})

	t.Run("reports the digest of the content, not the one the resource claims", func(t *testing.T) {
		claimed := &descriptor.Digest{HashAlgorithm: "SHA-256", Value: godigest.FromString("claimed").Encoded()}
		verified, err := VerifyDownload(t.Context(), resourceWithDigest(claimed),
			inmemory.New(strings.NewReader(verifyContent)))
		require.NoError(t, err)

		dig, ok := verified.(blob.DigestAware).Digest()
		require.True(t, ok)
		require.Equal(t, godigest.FromString(verifyContent).String(), dig)
	})

	t.Run("content that fails verification is not left at the target path", func(t *testing.T) {
		verified, err := VerifyDownload(t.Context(), resourceWithDigest(matching),
			inmemory.New(strings.NewReader("something else entirely")))
		require.NoError(t, err)

		out := filepath.Join(t.TempDir(), "out")
		require.ErrorContains(t, filesystem.CopyBlobToOSPath(verified, out), "digest mismatch")
		require.NoFileExists(t, out)
	})
}

// lazyMetadataBlob counts how often it is asked for its size or media type.
type lazyMetadataBlob struct {
	*inmemory.Blob
	asked int
}

func (b *lazyMetadataBlob) Size() int64 {
	b.asked++
	return b.Blob.Size()
}

func (b *lazyMetadataBlob) MediaType() (string, bool) {
	b.asked++
	return b.Blob.MediaType()
}
