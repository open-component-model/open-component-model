package verify

import (
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

func streamOf(content string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(content))
}

func calculateOnly(t *testing.T, content string) *VerifiedStreamBlob {
	t.Helper()
	b, err := NewVerifiedStreamBlob(streamOf(content), "")
	require.NoError(t, err)
	return b
}

func expecting(t *testing.T, content string, expected digest.Digest) *VerifiedStreamBlob {
	t.Helper()
	b, err := NewVerifiedStreamBlob(streamOf(content), expected)
	require.NoError(t, err)
	return b
}

func TestNewVerifiedStreamBlob_RejectsMalformedDigest(t *testing.T) {
	_, err := NewVerifiedStreamBlob(streamOf("content"), "sha256:not-a-digest")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid expected digest")
}

func TestVerifiedStreamBlob(t *testing.T) {
	const content = "hello world"

	t.Run("computes the digest on the fly while streaming", func(t *testing.T) {
		b := calculateOnly(t, content)

		_, known := b.Digest()
		assert.False(t, known, "the digest cannot be known before the stream is read")

		reader, err := b.ReadCloser()
		require.NoError(t, err)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		assert.Equal(t, content, string(data))
		require.NoError(t, reader.Close())

		computed, known := b.Digest()
		require.True(t, known, "the digest must be known once the stream is fully read")
		assert.Equal(t, digest.FromString(content).String(), computed)
	})

	t.Run("serves the stream exactly once", func(t *testing.T) {
		b := calculateOnly(t, content)

		reader, err := b.ReadCloser()
		require.NoError(t, err)
		require.NoError(t, reader.Close())

		_, err = b.ReadCloser()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already consumed")
	})

	t.Run("verifies a matching expected digest on close", func(t *testing.T) {
		b := expecting(t, content, digest.FromString(content))

		reader, err := b.ReadCloser()
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close(), "a matching digest must verify on close")
	})

	t.Run("rejects a mismatched expected digest on close", func(t *testing.T) {
		b := expecting(t, content, digest.FromString("something else"))

		reader, err := b.ReadCloser()
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, reader)
		require.NoError(t, err)
		err = reader.Close()
		require.ErrorIs(t, err, ErrMismatchedDigest)
		assert.Contains(t, err.Error(), "digest mismatch")
	})

	t.Run("a failed verification sticks to subsequent reads", func(t *testing.T) {
		b := expecting(t, content, digest.FromString("something else"))

		reader, err := b.ReadCloser()
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, reader)
		require.NoError(t, err)
		require.ErrorIs(t, reader.Close(), ErrMismatchedDigest)

		_, err = reader.Read(make([]byte, 1))
		assert.ErrorIs(t, err, ErrMismatchedDigest, "after a failed verification the reader must not hand out more content")
	})

	t.Run("rejects closing a partially read stream when a digest is expected", func(t *testing.T) {
		b := expecting(t, content, digest.FromString(content))

		reader, err := b.ReadCloser()
		require.NoError(t, err)
		_, err = reader.Read(make([]byte, 1))
		require.NoError(t, err)
		err = reader.Close()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "before being fully read")
	})

	t.Run("allows abandoning a partially read stream when no digest is expected", func(t *testing.T) {
		b := calculateOnly(t, content)

		reader, err := b.ReadCloser()
		require.NoError(t, err)
		_, err = reader.Read(make([]byte, 1))
		require.NoError(t, err)
		require.NoError(t, reader.Close(), "closing an abandoned calculate-only stream must not fail")

		_, known := b.Digest()
		assert.False(t, known, "an abandoned stream has no digest to report")
	})

	t.Run("reports the expected digest without reading", func(t *testing.T) {
		expected := digest.FromString(content)
		b := expecting(t, content, expected)

		reported, known := b.Digest()
		require.True(t, known, "an expected digest is known upfront")
		assert.Equal(t, expected.String(), reported)
	})

	t.Run("media type is settable and unknown by default", func(t *testing.T) {
		b := calculateOnly(t, content)

		_, known := b.MediaType()
		assert.False(t, known)

		b.SetMediaType("application/x-tgz")
		mt, known := b.MediaType()
		require.True(t, known)
		assert.Equal(t, "application/x-tgz", mt)
	})

	t.Run("size is unknown", func(t *testing.T) {
		b := calculateOnly(t, content)
		assert.Equal(t, blob.SizeUnknown, b.Size())
	})
}
