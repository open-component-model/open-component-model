package blob_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

type mockReadCloser struct {
	mock.Mock
}

func (m *mockReadCloser) Read(p []byte) (int, error) {
	args := m.Called(p)
	return args.Int(0), args.Error(1)
}

func (m *mockReadCloser) Close() error {
	args := m.Called()
	return args.Error(0)
}

func TestNewLockedReader(t *testing.T) {
	t.Run("should create reader and read data successfully", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()
		var mu sync.RWMutex

		testData := "hello world test data"
		testBlob := inmemory.New(strings.NewReader(testData))
		rc, err := testBlob.ReadCloser()
		r.NoError(err, "Getting ReadCloser should not error")

		lockedReader := blob.NewLockedReader(ctx, &mu, rc)
		r.NotNil(lockedReader, "lockedReader should not be nil")

		// Read data
		data, err := io.ReadAll(lockedReader)
		r.NoError(err, "Reading from lockedReader should not return error")
		r.Equal(testData, string(data), "Data should match original")

		// Close
		err = lockedReader.Close()
		r.NoError(err, "Closing lockedReader should not return error")
	})

	t.Run("should handle empty blob data without error", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()
		var mu sync.RWMutex

		testBlob := inmemory.New(strings.NewReader(""))
		rc, err := testBlob.ReadCloser()
		r.NoError(err, "Getting ReadCloser should not error")

		lockedReader := blob.NewLockedReader(ctx, &mu, rc)
		r.NotNil(lockedReader, "lockedReader should not be nil")

		// Read data
		data, err := io.ReadAll(lockedReader)
		r.NoError(err, "Reading from empty lockedReader should not return error")
		r.Equal("", string(data), "Empty blob should return empty data")

		// Close
		err = lockedReader.Close()
		r.NoError(err, "Closing lockedReader should not return error")
	})

	t.Run("should propagate error when underlying reader fails during copy", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()
		var mu sync.RWMutex

		failingReadCloser := new(mockReadCloser)
		failingReadCloser.On("Read", mock.Anything).Return(0, errors.New("simulated read failure"))
		failingReadCloser.On("Close").Return(nil)

		lockedReader := blob.NewLockedReader(ctx, &mu, failingReadCloser)

		_, err := io.ReadAll(lockedReader)
		r.Error(err)
		r.Equal("unable to copy data: simulated read failure", err.Error())
	})

	t.Run("should handle concurrent access", func(t *testing.T) {
		r := require.New(t)
		ctx := context.Background()
		var mu sync.RWMutex

		testData := "concurrent test data"
		const numReaders = 5

		eg, ctx := errgroup.WithContext(ctx)
		for i := 0; i < numReaders; i++ {
			eg.Go(func() error {
				localBlob := inmemory.New(strings.NewReader(testData))
				rc, err := localBlob.ReadCloser()
				if err != nil {
					return fmt.Errorf("getting ReadCloser failed: %w", err)
				}

				lockedReader := blob.NewLockedReader(ctx, &mu, rc)

				if _, err := io.ReadAll(lockedReader); err != nil {
					return fmt.Errorf("ReadAll failed for reader %d: %w", i, err)
				}

				return lockedReader.Close()
			})
		}

		err := eg.Wait()
		r.NoError(err, "All concurrent readers should complete without error")
	})

	t.Run("should interrupt pipe when context is cancelled", func(t *testing.T) {
		r := require.New(t)
		ctx, cancel := context.WithCancel(context.Background())
		var mu sync.RWMutex

		// Create slow reader
		slowReadCloser := new(mockReadCloser)

		// Mock Read that blocks until context is cancelled
		slowReadCloser.On("Read", mock.Anything).After(10*time.Millisecond).Return(9, nil)

		// Close might not be called if context cancellation happens before defer cleanup is reached
		slowReadCloser.On("Close").Return(nil).Once()

		lockedReader := blob.NewLockedReader(ctx, &mu, slowReadCloser)

		// Cancel context after a brief delay
		go func() {
			time.Sleep(10 * time.Millisecond)
			cancel()
		}()

		// Try to read - should get cancellation error due to separate goroutine interrupting pipe
		_, err := io.ReadAll(lockedReader)
		r.Error(err, "Reading should return error when context is cancelled")
		r.ErrorContains(err, "context canceled", "Error should indicate context cancellation")

		// Close should not error
		closeErr := lockedReader.Close()
		r.NoError(closeErr, "Close should not return error after cancellation")

		// Wait for lockedReader's internal goroutines to complete
		// The implementation has 2 goroutines: context cancellation + copy goroutine
		time.Sleep(50 * time.Millisecond)

		slowReadCloser.AssertExpectations(t)
	})
}
