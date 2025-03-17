package blob_test

import (
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"

	"ocm.software/open-component-model/bindings/go/blob"
)

func TestBufferedReader(t *testing.T) {
	data := "hello world"
	r := strings.NewReader(data)
	br := blob.NewEagerBufferedReader(r)

	t.Run("Test Read", func(t *testing.T) {
		buf := make([]byte, len(data))
		n, err := br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, string(buf))
	})

	t.Run("Test Digest Calculation After Read", func(t *testing.T) {
		br = blob.NewEagerBufferedReader(strings.NewReader(data))
		buf := make([]byte, len(data)/2)
		br.Read(buf) // Partial read
		expectedDigest, err := digest.FromReader(strings.NewReader(data))
		assert.NoError(t, err)
		dig, known := br.Digest()
		assert.True(t, known)
		assert.Equal(t, expectedDigest.String(), dig)
	})

	t.Run("Test Digest Calculation Before Read", func(t *testing.T) {
		br = blob.NewEagerBufferedReader(strings.NewReader(data))
		expectedDigest, err := digest.FromReader(strings.NewReader(data))
		assert.NoError(t, err)
		dig, known := br.Digest()
		assert.True(t, known)
		assert.Equal(t, expectedDigest.String(), dig)
	})

	t.Run("Test Size Calculation After Read", func(t *testing.T) {
		br = blob.NewEagerBufferedReader(strings.NewReader(data))
		buf := make([]byte, len(data)/2)
		br.Read(buf) // Partial read
		expectedSize := uint64(len(data))
		actualSize, known := br.Size()
		assert.True(t, known)
		assert.Equal(t, expectedSize, actualSize)
	})

	t.Run("Test Size Calculation Before Read", func(t *testing.T) {
		br = blob.NewEagerBufferedReader(strings.NewReader(data))
		expectedSize := uint64(len(data))
		actualSize, known := br.Size()
		assert.True(t, known)
		assert.Equal(t, expectedSize, actualSize)
	})

	t.Run("Test Precalculated Digest", func(t *testing.T) {
		br.SetPrecalculatedDigest("test-digest")
		assert.True(t, br.HasPrecalculatedDigest())
		dig, known := br.Digest()
		assert.True(t, known)
		assert.Equal(t, "test-digest", dig)
	})

	t.Run("Test Precalculated Digest Not Set", func(t *testing.T) {
		br = blob.NewEagerBufferedReader(strings.NewReader(data))
		assert.False(t, br.HasPrecalculatedDigest())
		assert.NoError(t, br.LoadEagerly())
		assert.True(t, br.HasPrecalculatedDigest())
		dig, known := br.Digest()
		assert.True(t, known)
		expectedDigest, err := digest.FromReader(strings.NewReader(data))
		assert.NoError(t, err)
		assert.Equal(t, expectedDigest.String(), dig)
	})

	t.Run("Test Precalculated Size", func(t *testing.T) {
		br = blob.NewEagerBufferedReader(strings.NewReader(data))
		assert.False(t, br.HasPrecalculatedSize())
		assert.NoError(t, br.LoadEagerly())
		assert.True(t, br.HasPrecalculatedSize())
		size, known := br.Size()
		assert.True(t, known)
		expectedSize := uint64(len(data))
		assert.Equal(t, expectedSize, size)
	})

	t.Run("Test MediaType", func(t *testing.T) {
		mediaType, known := br.MediaType()
		assert.True(t, known)
		assert.Equal(t, "application/octet-stream", mediaType)

		br.SetMediaType("application/text")
		mediaType, known = br.MediaType()
		assert.True(t, known)
		assert.Equal(t, "application/text", mediaType)
	})

	t.Run("Test Close", func(t *testing.T) {
		closableReader := io.NopCloser(strings.NewReader(data))
		br := blob.NewEagerBufferedReader(closableReader)
		err := br.Close()
		assert.NoError(t, err)
	})
}
