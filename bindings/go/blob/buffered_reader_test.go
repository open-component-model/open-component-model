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

func TestPartialReadCloseDigestError(t *testing.T) {
	data := []byte("test data")
	buffered := blob.NewDirectReadOnlyBlob(bytes.NewReader(data))

	r := require.New(t)
	reader, err := buffered.ReadCloser()
	r.NoError(err)
	defer reader.Close()

	partial := make([]byte, 4)
	n, err := reader.Read(partial)
	r.NoError(err)
	r.Equal(4, n)
	r.Equal(data[:4], partial)

	// Close the reader
	err = reader.Close()
	r.NoError(err)

	// make sure the digest returned is the complete content digest and not just the partial read
	digRaw, ok := buffered.Digest()
	r.True(ok)
	dig, err := digest.Parse(digRaw)
	r.NoError(err)

	r.NotEqual(digest.FromBytes(partial), dig)
}

func TestBufferedReader_Seek(t *testing.T) {
	data := "hello world"
	br := blob.NewEagerBufferedReader(strings.NewReader(data))

	t.Run("SeekStart", func(t *testing.T) {
		// Seek to start
		pos, err := br.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), pos)

		// Read first character
		buf := make([]byte, 1)
		n, err := br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 1, n)
		assert.Equal(t, "h", string(buf))

		// Seek to middle
		pos, err = br.Seek(5, io.SeekStart)
		assert.NoError(t, err)
		assert.Equal(t, int64(5), pos)

		// Read from middle
		buf = make([]byte, 2)
		n, err = br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 2, n)
		assert.Equal(t, " w", string(buf))
	})

	t.Run("SeekCurrent", func(t *testing.T) {
		// Reset to start
		_, err := br.Seek(0, io.SeekStart)
		assert.NoError(t, err)

		// Read first character
		buf := make([]byte, 1)
		n, err := br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 1, n)

		// Seek forward 2 positions from current
		pos, err := br.Seek(2, io.SeekCurrent)
		assert.NoError(t, err)
		assert.Equal(t, int64(3), pos)

		// Read from new position
		buf = make([]byte, 2)
		n, err = br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 2, n)
		assert.Equal(t, "lo", string(buf))

		// Seek backward 1 position
		pos, err = br.Seek(-1, io.SeekCurrent)
		assert.NoError(t, err)
		assert.Equal(t, int64(4), pos)

		// Read from new position
		buf = make([]byte, 1)
		n, err = br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 1, n)
		assert.Equal(t, "o", string(buf))
	})

	t.Run("SeekEnd", func(t *testing.T) {
		// Seek to end
		pos, err := br.Seek(0, io.SeekEnd)
		assert.NoError(t, err)
		assert.Equal(t, int64(len(data)), pos)

		// Seek back 3 positions from end
		pos, err = br.Seek(-3, io.SeekEnd)
		assert.NoError(t, err)
		assert.Equal(t, int64(len(data)-3), pos)

		// Read from new position
		buf := make([]byte, 3)
		n, err := br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 3, n)
		assert.Equal(t, "rld", string(buf))
	})

	t.Run("Edge Cases", func(t *testing.T) {
		// Test seeking beyond buffer length
		pos, err := br.Seek(int64(len(data)+1), io.SeekStart)
		assert.Error(t, err) // Should return an error
		assert.Equal(t, io.ErrUnexpectedEOF, err)

		// Test seeking to negative position
		pos, err = br.Seek(-1, io.SeekStart)
		assert.Error(t, err)
		assert.Equal(t, io.ErrUnexpectedEOF, err)

		// Test seeking with invalid whence
		pos, err = br.Seek(0, 999)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), pos) // Should default to SeekStart
	})

	t.Run("Seek and Read Sequence", func(t *testing.T) {
		// Reset to start
		_, err := br.Seek(0, io.SeekStart)
		assert.NoError(t, err)

		// Read first 5 bytes
		buf := make([]byte, 5)
		n, err := br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "hello", string(buf))

		// Seek to position 6
		pos, err := br.Seek(6, io.SeekStart)
		assert.NoError(t, err)
		assert.Equal(t, int64(6), pos)

		// Read remaining bytes
		buf = make([]byte, 5)
		n, err = br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, 5, n)
		assert.Equal(t, "world", string(buf))

		// Seek back to start and verify we can read again
		pos, err = br.Seek(0, io.SeekStart)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), pos)

		buf = make([]byte, len(data))
		n, err = br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, string(buf))
	})
}
