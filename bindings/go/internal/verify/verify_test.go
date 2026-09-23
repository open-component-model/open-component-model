package verify_test

import (
	"io"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/internal/verify"
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
		verified, err := verify.Download(t.Context(), resourceWithDigest(matching),
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
		verified, err := verify.Download(t.Context(), resourceWithDigest(matching),
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
			verified, err := verify.Download(t.Context(), resourceWithDigest(dig), content)
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
			_, err := verify.Download(t.Context(), resourceWithDigest(dig),
				inmemory.New(strings.NewReader(verifyContent)))
			require.Error(t, err, "digest %+v must not pass as verifiable", dig)
		}
	})

	t.Run("releases content it refuses to verify", func(t *testing.T) {
		content := &closableBlob{Blob: inmemory.New(strings.NewReader(verifyContent))}
		_, err := verify.Download(t.Context(), resourceWithDigest(&descriptor.Digest{HashAlgorithm: "SHA-256"}), content)
		require.Error(t, err)
		require.True(t, content.closed, "the temporary file behind a refused download must not be left behind")
	})
}
