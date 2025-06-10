package blob_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		expectedSize := int64(len(data))
		size := br.Size()
		assert.Greater(t, size, int64(0))
		assert.Equal(t, expectedSize, size)
	})

	t.Run("Test Size Calculation Before Read", func(t *testing.T) {
		br = blob.NewEagerBufferedReader(strings.NewReader(data))
		expectedSize := int64(len(data))
		size := br.Size()
		assert.Greater(t, size, int64(0))
		assert.Equal(t, expectedSize, size)
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
		size := br.Size()
		assert.Greater(t, size, int64(0))
		expectedSize := int64(len(data))
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

func TestBufferMemory_RepeatedReads(t *testing.T) {
	data := []byte("test data")
	var err error
	buffered := blob.NewDirectReadOnlyBlob(bytes.NewReader(data))

	t.Run("First Read", func(t *testing.T) {
		r := require.New(t)
		var buf1 bytes.Buffer
		err = blob.Copy(&buf1, buffered)
		r.NoError(err)
		r.Equal(data, buf1.Bytes())
	})

	t.Run("Second Read", func(t *testing.T) {
		r := require.New(t)
		var buf2 bytes.Buffer
		err = blob.Copy(&buf2, buffered)
		r.NoError(err)
		r.Equal(data, buf2.Bytes())
	})

	// Third read with partial read
	t.Run("Third Read with Partial Read", func(t *testing.T) {
		r := require.New(t)
		reader, err := buffered.ReadCloser()
		r.NoError(err)
		defer reader.Close()

		partial := make([]byte, 4)
		n, err := reader.Read(partial)
		r.NoError(err)
		r.Equal(4, n)
		r.Equal(data[:4], partial)

		// Read the rest
		rest := make([]byte, len(data)-4)
		n, err = reader.Read(rest)
		r.NoError(err)
		r.Equal(len(data)-4, n)
		r.Equal(data[4:], rest)
	})

	// Final read to ensure all data is read after partial read completed
	t.Run("Final Read After Partial Read", func(t *testing.T) {
		r := require.New(t)
		var buf3 bytes.Buffer
		err = blob.Copy(&buf3, buffered)
		r.NoError(err)
		r.Equal(data, buf3.Bytes())
	})
}
