package direct

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob"
)

func TestReader(t *testing.T) {
	data := "hello world"
	r := strings.NewReader(data)
	br := New(r)

	t.Run("Test Read", func(t *testing.T) {
		s := br.Size()

		buf := make([]byte, len(data))
		br, err := br.ReadCloser()
		assert.NoError(t, err)
		t.Cleanup(func() {
			assert.NoError(t, br.Close())
		})
		reader := io.LimitReader(br, s)
		n, err := reader.Read(buf)
		assert.NoError(t, err)
		assert.Equal(t, len(data), n)
		assert.Equal(t, data, string(buf))
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
	//
	t.Run("Test Size Calculation Before Read", func(t *testing.T) {
		br = New(strings.NewReader(data))
		expectedSize := int64(len(data))
		size := br.Size()
		assert.Greater(t, size, int64(0))
		assert.Equal(t, expectedSize, size)
	})
	//
	t.Run("Test MediaType", func(t *testing.T) {
		mediaType, known := br.MediaType()
		assert.True(t, known)
		assert.Equal(t, "application/octet-stream", mediaType)

		br.SetMediaType("application/text")
		mediaType, known = br.MediaType()
		assert.True(t, known)
		assert.Equal(t, "application/text", mediaType)
	})
}

func TestBlobOptions(t *testing.T) {
	data := "test data"

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
	})

}

func TestCompatibility(t *testing.T) {
	data := "data"

	t.Run("strings", func(t *testing.T) {
		closableReader := strings.NewReader(data)
		b := New(closableReader)
		br, err := b.ReadCloser()
		assert.NoError(t, err)
		assert.NoError(t, br.Close())
		assert.Equal(t, int64(len(data)), b.Size())
	})

	t.Run("bytes", func(t *testing.T) {
		t.Run("direct", func(t *testing.T) {
			b := NewFromBytes([]byte(data))
			br, err := b.ReadCloser()
			assert.NoError(t, err)
			result, err := io.ReadAll(br)
			assert.NoError(t, err)
			assert.Equal(t, data, string(result))
			assert.Equal(t, int64(len(data)), b.Size())
		})
		t.Run("reader", func(t *testing.T) {
			b := New(bytes.NewReader([]byte(data)))
			br, err := b.ReadCloser()
			assert.NoError(t, err)
			result, err := io.ReadAll(br)
			assert.NoError(t, err)
			assert.Equal(t, data, string(result))
			assert.Equal(t, int64(len(data)), b.Size())
		})
	})

	t.Run("buffer", func(t *testing.T) {
		run := func(t assert.TestingT, unsafe bool) {
			b := NewFromBuffer(bytes.NewBuffer([]byte(data)), unsafe)
			br, err := b.ReadCloser()
			assert.NoError(t, err)
			result, err := io.ReadAll(br)
			assert.NoError(t, err)
			assert.Equal(t, data, string(result))
			assert.Equal(t, int64(len(data)), b.Size())
		}
		run(t, false)
		run(t, true)
	})

	// Note that file compatibility is discouraged because we also have a full
	// file implementation support in the filesystem package which directly works
	// with stat calls on the underlying filesystem instead of seeking the size.
	t.Run("file", func(t *testing.T) {
		// Create a temporary file with the data
		tmpFile, err := os.CreateTemp(t.TempDir(), "")
		assert.NoError(t, err)
		defer func() {
			assert.NoError(t, tmpFile.Close())
		}()

		_, err = tmpFile.Write([]byte(data))
		assert.NoError(t, err)

		b := New(tmpFile)
		br, err := b.ReadCloser()
		assert.NoError(t, err)
		result, err := io.ReadAll(br)
		assert.NoError(t, err)
		assert.Equal(t, data, string(result))
		assert.Equal(t, int64(len(data)), b.Size())
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

func TestConcurrentAndSerialReads(t *testing.T) {
	data := "test data for concurrent and serial reads"
	blob := New(strings.NewReader(data))
	expectedData := []byte(data)

	t.Run("Serial Reads", func(t *testing.T) {
		r := require.New(t)
		// Perform multiple serial reads
		for range 5 {
			reader, err := blob.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() {
				r.NoError(reader.Close())
			})

			buf := make([]byte, len(data))
			n, err := reader.Read(buf)
			r.NoError(err)
			r.Equal(len(data), n)
			r.Equal(expectedData, buf)
		}
	})

	t.Run("Concurrent Reads", func(t *testing.T) {
		r := require.New(t)
		const numGoroutines = 10
		done := make(chan struct{})

		for range numGoroutines {
			go func() {
				defer func() { done <- struct{}{} }()

				reader, err := blob.ReadCloser()
				r.NoError(err)
				defer reader.Close()

				buf := make([]byte, len(data))
				n, err := reader.Read(buf)
				r.NoError(err)
				r.Equal(len(data), n)
				r.Equal(expectedData, buf)
			}()
		}

		// Wait for all goroutines to complete
		for range numGoroutines {
			<-done
		}
	})

	t.Run("Mixed Concurrent and Serial Reads", func(t *testing.T) {
		r := require.New(t)
		const numGoroutines = 5
		done := make(chan struct{})

		// Start concurrent reads
		for range numGoroutines {
			go func() {
				defer func() { done <- struct{}{} }()

				reader, err := blob.ReadCloser()
				r.NoError(err)
				defer reader.Close()

				buf := make([]byte, len(data))
				n, err := reader.Read(buf)
				r.NoError(err)
				r.Equal(len(data), n)
				r.Equal(expectedData, buf)
			}()
		}

		// Perform serial reads while concurrent reads are happening
		for range 3 {
			reader, err := blob.ReadCloser()
			r.NoError(err)
			t.Cleanup(func() {
				r.NoError(reader.Close())
			})

			buf := make([]byte, len(data))
			n, err := reader.Read(buf)
			r.NoError(err)
			r.Equal(len(data), n)
			r.Equal(expectedData, buf)
		}

		// Wait for all goroutines to complete
		for range numGoroutines {
			<-done
		}
	})

	t.Run("Concurrent Partial Reads", func(t *testing.T) {
		r := require.New(t)
		const numGoroutines = 5
		done := make(chan struct{})

		for range numGoroutines {
			go func() {
				defer func() { done <- struct{}{} }()

				reader, err := blob.ReadCloser()
				r.NoError(err)
				defer reader.Close()

				// Read first half
				firstHalf := make([]byte, len(data)/2)
				n, err := reader.Read(firstHalf)
				r.NoError(err)
				r.Equal(len(data)/2, n)
				r.Equal(expectedData[:len(data)/2], firstHalf)

				// Read second half
				secondHalf := make([]byte, len(data)-len(data)/2)
				n, err = reader.Read(secondHalf)
				r.NoError(err)
				r.Equal(len(data)-len(data)/2, n)
				r.Equal(expectedData[len(data)/2:], secondHalf)
			}()
		}

		// Wait for all goroutines to complete
		for range numGoroutines {
			<-done
		}
	})
}
