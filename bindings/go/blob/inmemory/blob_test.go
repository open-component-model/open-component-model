package inmemory_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	. "ocm.software/open-component-model/bindings/go/blob/inmemory"
)

func Test_ReadCloserReturnsReader(t *testing.T) {
	r := require.New(t)
	reader := strings.NewReader("test data")
	blob := New(reader)
	readCloser, err := blob.ReadCloser()
	r.NoError(err)
	r.NotNil(readCloser)
}

func Test_ReadCloserReadsDataCorrectly(t *testing.T) {
	r := require.New(t)
	expectedData := "test data"
	reader := strings.NewReader(expectedData)
	blob := New(reader)
	readCloser, err := blob.ReadCloser()
	r.NoError(err)
	data, err := io.ReadAll(readCloser)
	r.NoError(err)
	r.Equal(expectedData, string(data))
}

func Test_ReadCloserHandlesEmptyReader(t *testing.T) {
	r := require.New(t)
	reader := strings.NewReader("")
	blob := New(reader)
	readCloser, err := blob.ReadCloser()
	r.NoError(err)
	data, err := io.ReadAll(readCloser)
	r.NoError(err)
	r.Equal("", string(data))
}

func TestBufferedReader(t *testing.T) {
	data := "hello world"
	r := strings.NewReader(data)
	br := New(r)

	t.Run("Test Read", func(t *testing.T) {
		buf := make([]byte, len(data))
		br, err := br.ReadCloser()
		assert.NoError(t, err)
		t.Cleanup(func() {
			assert.NoError(t, br.Close())
		})
		n, err := br.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, string(buf))
	})

	t.Run("Test Digest Calculation After Read", func(t *testing.T) {
		b := New(strings.NewReader(data))
		br, err := b.ReadCloser()
		assert.NoError(t, err)
		t.Cleanup(func() {
			assert.NoError(t, br.Close())
		})
		buf := make([]byte, len(data)/2)
		br.Read(buf) // Partial read
		expectedDigest, err := digest.FromReader(strings.NewReader(data))
		assert.NoError(t, err)
		dig, known := b.Digest()
		assert.True(t, known)
		assert.Equal(t, expectedDigest.String(), dig)
	})

	t.Run("Test Digest Calculation Before Read", func(t *testing.T) {
		br = New(strings.NewReader(data))
		expectedDigest, err := digest.FromReader(strings.NewReader(data))
		assert.NoError(t, err)
		dig, known := br.Digest()
		assert.True(t, known)
		assert.Equal(t, expectedDigest.String(), dig)
	})

	t.Run("Test Size Calculation After Read", func(t *testing.T) {
		b := New(strings.NewReader(data))
		br, err := b.ReadCloser()
		assert.NoError(t, err)
		t.Cleanup(func() {
			assert.NoError(t, br.Close())
		})
		buf := make([]byte, len(data)/2)
		br.Read(buf) // Partial read
		expectedSize := int64(len(data))
		size := b.Size()
		assert.Greater(t, size, int64(0))
		assert.Equal(t, expectedSize, size)
	})

	t.Run("Test Size Calculation Before Read", func(t *testing.T) {
		br = New(strings.NewReader(data))
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
		br = New(strings.NewReader(data))
		assert.False(t, br.HasPrecalculatedDigest())
		assert.NoError(t, br.Load())
		assert.True(t, br.HasPrecalculatedDigest())
		dig, known := br.Digest()
		assert.True(t, known)
		expectedDigest, err := digest.FromReader(strings.NewReader(data))
		assert.NoError(t, err)
		assert.Equal(t, expectedDigest.String(), dig)
	})

	t.Run("Test Precalculated Size", func(t *testing.T) {
		br = New(strings.NewReader(data))
		assert.False(t, br.HasPrecalculatedSize())
		assert.NoError(t, br.Load())
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
		b := New(closableReader)
		br, err := b.ReadCloser()
		assert.NoError(t, err)
		assert.NoError(t, br.Close())
	})
}

func TestBufferMemory_RepeatedReads(t *testing.T) {
	data := []byte("test data")
	var err error
	buffered := New(bytes.NewReader(data))

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
	buffered := New(bytes.NewReader(data))

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

func TestMemoryBlobOptions(t *testing.T) {
	data := "test data"
	expectedDigest, err := digest.FromReader(strings.NewReader(data))
	require.NoError(t, err)

	t.Run("Test WithMediaType Option", func(t *testing.T) {
		r := require.New(t)
		blob := New(strings.NewReader(data), WithMediaType("application/text"))
		mediaType, known := blob.MediaType()
		r.True(known)
		r.Equal("application/text", mediaType)
	})

	t.Run("Test WithSize Option", func(t *testing.T) {
		t.Run("With Full known Size", func(t *testing.T) {
			r := require.New(t)
			expectedSize := int64(len(data))
			blob := New(strings.NewReader(data), WithSize(expectedSize))
			r.True(blob.HasPrecalculatedSize())
			r.Equal(expectedSize, blob.Size())

			rc, err := blob.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() {
				r.NoError(rc.Close())
			})
			d, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(data, string(d))
		})

		t.Run("With Limited known Size", func(t *testing.T) {
			r := require.New(t)
			expectedSize := int64(len(data) - 1)
			blob := New(strings.NewReader(data), WithSize(expectedSize))
			r.True(blob.HasPrecalculatedSize())
			r.Equal(expectedSize, blob.Size())

			rc, err := blob.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() {
				r.NoError(rc.Close())
			})
			d, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(data[:len(data)-1], string(d))
		})

		t.Run("Test Too Large Size", func(t *testing.T) {
			r := require.New(t)
			// Create blob with incorrect size
			incorrectSize := int64(999)
			blob := New(strings.NewReader(data), WithSize(incorrectSize))

			// Loading should fail because the actual data size doesn't match the provided size
			err := blob.Load()
			r.Error(err)
			r.ErrorIs(err, io.EOF)
		})
	})

	t.Run("Test WithDigest Option", func(t *testing.T) {
		t.Run("matching digest", func(t *testing.T) {
			r := require.New(t)
			blob := New(strings.NewReader(data), WithDigest(expectedDigest.String()))
			r.True(blob.HasPrecalculatedDigest())
			dig, known := blob.Digest()
			r.True(known)
			r.Equal(expectedDigest.String(), dig)

			rc, err := blob.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() {
				r.NoError(rc.Close())
			})
		})

		t.Run("non-matching digest", func(t *testing.T) {
			r := require.New(t)
			blob := New(strings.NewReader(data), WithDigest(digest.FromString("bla")))
			r.True(blob.HasPrecalculatedDigest())

			_, known := blob.Digest()
			r.False(known)

			_, err := blob.ReadCloser()
			r.Error(err)
			r.ErrorContains(err, "differed from loaded digest")
		})
	})

	t.Run("Test Multiple Options Together", func(t *testing.T) {
		r := require.New(t)
		expectedSize := int64(len(data))
		blob := New(strings.NewReader(data),
			WithMediaType("application/text"),
			WithSize(expectedSize),
			WithDigest(expectedDigest.String()))

		// Verify all options were applied correctly
		mediaType, known := blob.MediaType()
		r.True(known)
		r.Equal("application/text", mediaType)

		r.True(blob.HasPrecalculatedSize())
		r.Equal(expectedSize, blob.Size())

		r.True(blob.HasPrecalculatedDigest())
		dig, known := blob.Digest()
		r.True(known)
		r.Equal(expectedDigest.String(), dig)
	})

}
