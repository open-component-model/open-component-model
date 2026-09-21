package repository_test

import (
	"io"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
)

const verifyContent = "content the descriptor took a digest over"

func resourceWithDigest(dig *descriptor.Digest) *descriptor.Resource {
	res := &descriptor.Resource{}
	res.Name = "test-resource"
	res.Version = "1.0.0"
	res.Digest = dig
	return res
}

func TestNewVerifyingBlob(t *testing.T) {
	matching := &descriptor.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: "genericBlobDigest/v1",
		Value:                  godigest.FromString(verifyContent).Encoded(),
	}

	t.Run("holds content to a matching digest", func(t *testing.T) {
		verifying, err := repository.NewVerifyingBlob(resourceWithDigest(matching),
			inmemory.New(strings.NewReader(verifyContent)))
		require.NoError(t, err)
		verified, err := verifying.Verify(t.Context())
		require.NoError(t, err)
		require.True(t, verified)

		rc, err := verifying.ReadCloser()
		require.NoError(t, err)
		read, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		require.Equal(t, verifyContent, string(read))
	})

	t.Run("reports content that does not match", func(t *testing.T) {
		verifying, err := repository.NewVerifyingBlob(resourceWithDigest(matching),
			inmemory.New(strings.NewReader("something else entirely")))
		require.NoError(t, err)
		_, err = verifying.Verify(t.Context())
		require.ErrorContains(t, err, "digest mismatch")
	})

	t.Run("has nothing to verify when the resource carries no digest", func(t *testing.T) {
		for _, dig := range []*descriptor.Digest{
			nil,
			{HashAlgorithm: "SHA-256"},
			{HashAlgorithm: descriptor.NoDigest, NormalisationAlgorithm: descriptor.ExcludeFromSignature, Value: descriptor.NoDigest},
		} {
			// Still a VerifyingBlob, so a failed type assertion at a call site means
			// "this did not come from a verifying repository" and nothing else.
			verifying, err := repository.NewVerifyingBlob(resourceWithDigest(dig),
				inmemory.New(strings.NewReader(verifyContent)))
			require.NoError(t, err)
			verified, err := verifying.Verify(t.Context())
			require.NoError(t, err)
			require.False(t, verified, "nothing to verify must not report as verified")
		}
	})

	t.Run("refuses content whose digest is present but unusable", func(t *testing.T) {
		for _, dig := range []*descriptor.Digest{
			{HashAlgorithm: "MD5", Value: godigest.FromString(verifyContent).Encoded()},
			{HashAlgorithm: "SHA-512", Value: godigest.SHA512.FromString(verifyContent).Encoded()},
			{HashAlgorithm: "SHA-256", Value: "not-hex"},
			{HashAlgorithm: "SHA-256", Value: "abcd"},
		} {
			// Unusable is not "nothing to verify": the download must refuse it rather
			// than hand back content that only fails later.
			_, err := repository.NewVerifyingBlob(resourceWithDigest(dig),
				inmemory.New(strings.NewReader(verifyContent)))
			require.Error(t, err, "digest %+v must not pass as verifiable", dig)
		}
	})
}
