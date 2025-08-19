package direct

import (
	"errors"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockBlob implements blob.ReadOnlyBlob for testing
type mockBlob struct {
	data             string
	readCloserCalled int
	mu               sync.Mutex
	readCloserErr    error
}

func (m *mockBlob) ReadCloser() (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.readCloserCalled++

	if m.readCloserErr != nil {
		return nil, m.readCloserErr
	}

	return io.NopCloser(strings.NewReader(m.data)), nil
}

func (m *mockBlob) getReadCloserCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.readCloserCalled
}

func TestNewReadCloserWrapper(t *testing.T) {
	blob := &mockBlob{data: "hello world"}
	wrapper := NewReadCloserWrapper(blob)

	assert.NotNil(t, wrapper)
	assert.IsType(t, &readCloserWrapper{}, wrapper)
}

func TestReadCloserWrapper_LazyOpening(t *testing.T) {
	blob := &mockBlob{data: "hello world"}
	wrapper := NewReadCloserWrapper(blob)

	// Verify blob.ReadCloser() hasn't been called yet
	assert.Equal(t, 0, blob.getReadCloserCalls())

	// First read should open the blob
	buf := make([]byte, 5)
	n, err := wrapper.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", string(buf))
	assert.Equal(t, 1, blob.getReadCloserCalls())

	// Second read should use the already opened reader
	n, err = wrapper.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, " worl", string(buf))
	assert.Equal(t, 1, blob.getReadCloserCalls()) // Still only called once
}

func TestReadCloserWrapper_FullRead(t *testing.T) {
	testData := "hello world test data"
	blob := &mockBlob{data: testData}
	wrapper := NewReadCloserWrapper(blob)

	// Read all data
	data, err := io.ReadAll(wrapper)
	require.NoError(t, err)
	assert.Equal(t, testData, string(data))
	assert.Equal(t, 1, blob.getReadCloserCalls())

	// Close the wrapper
	err = wrapper.Close()
	assert.NoError(t, err)
}

func TestReadCloserWrapper_ErrorOnBlobOpen(t *testing.T) {
	expectedErr := errors.New("failed to open blob")
	blob := &mockBlob{
		data:          "hello world",
		readCloserErr: expectedErr,
	}
	wrapper := NewReadCloserWrapper(blob)

	buf := make([]byte, 5)
	n, err := wrapper.Read(buf)

	assert.Equal(t, 0, n)
	assert.Equal(t, expectedErr, err)
	assert.Equal(t, 1, blob.getReadCloserCalls())
}

func TestReadCloserWrapper_CloseWithoutRead(t *testing.T) {
	blob := &mockBlob{data: "hello world"}
	wrapper := NewReadCloserWrapper(blob)

	// Close without reading should not error
	err := wrapper.Close()
	assert.NoError(t, err)
	assert.Equal(t, 0, blob.getReadCloserCalls())

	// Reading after close should return error
	buf := make([]byte, 5)
	n, err := wrapper.Read(buf)
	assert.Equal(t, 0, n)
	assert.Equal(t, io.ErrClosedPipe, err)
}

func TestReadCloserWrapper_ReadAfterClose(t *testing.T) {
	blob := &mockBlob{data: "hello world"}
	wrapper := NewReadCloserWrapper(blob)

	// First read to open the blob
	buf := make([]byte, 5)
	n, err := wrapper.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, "hello", string(buf))

	// Close the wrapper
	err = wrapper.Close()
	assert.NoError(t, err)

	// Reading after close should return error
	n, err = wrapper.Read(buf)
	assert.Equal(t, 0, n)
	assert.Equal(t, io.ErrClosedPipe, err)
}

func TestReadCloserWrapper_MultipleClose(t *testing.T) {
	blob := &mockBlob{data: "hello world"}
	wrapper := NewReadCloserWrapper(blob)

	// First read to open the blob
	buf := make([]byte, 5)
	_, err := wrapper.Read(buf)
	require.NoError(t, err)

	// Multiple closes should not error
	err = wrapper.Close()
	assert.NoError(t, err)

	err = wrapper.Close()
	assert.NoError(t, err)

	err = wrapper.Close()
	assert.NoError(t, err)
}

func TestReadCloserWrapper_ConcurrentAccess(t *testing.T) {
	blob := &mockBlob{data: strings.Repeat("hello world ", 100)}
	wrapper := NewReadCloserWrapper(blob)

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Start multiple goroutines reading concurrently
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 10)
			for j := 0; j < 10; j++ {
				_, err := wrapper.Read(buf)
				if err != nil && err != io.EOF {
					errors <- err
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent read error: %v", err)
	}

	// Blob should only be opened once despite concurrent access
	assert.Equal(t, 1, blob.getReadCloserCalls())

	err := wrapper.Close()
	assert.NoError(t, err)
}

func TestReadCloserWrapper_WithDirectBlob(t *testing.T) {
	testData := "hello world from direct blob"
	directBlob := NewFromBytes([]byte(testData))
	wrapper := NewReadCloserWrapper(directBlob)

	data, err := io.ReadAll(wrapper)
	require.NoError(t, err)
	assert.Equal(t, testData, string(data))

	err = wrapper.Close()
	assert.NoError(t, err)
}
