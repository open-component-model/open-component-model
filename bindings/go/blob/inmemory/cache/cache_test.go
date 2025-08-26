package cache_test

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"

	"ocm.software/open-component-model/bindings/go/blob"
	. "ocm.software/open-component-model/bindings/go/blob/inmemory/cache"
)

type mockBlob struct {
	data        []byte
	mediaType   string
	unknownSize bool
}

func (m *mockBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(m.data)), nil
}

func (m *mockBlob) Size() int64 {
	if m.unknownSize {
		return blob.SizeUnknown
	}
	return int64(len(m.data))
}

func (m *mockBlob) MediaType() (string, bool) {
	if m.mediaType != "" {
		return m.mediaType, true
	}
	return "", false
}

func TestCache_ReadCloser(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		mediaType   string
		unknownSize bool
	}{
		{
			name:      "empty blob",
			data:      []byte{},
			mediaType: "",
		},
		{
			name:      "small blob",
			data:      []byte("hello"),
			mediaType: "text/plain",
		},
		{
			name:      "large blob",
			data:      bytes.Repeat([]byte("x"), 1024*1024), // 1MB
			mediaType: "application/octet-stream",
		},
		{
			name:        "unknown size empty blob",
			data:        []byte{},
			mediaType:   "",
			unknownSize: true,
		},
		{
			name:        "unknown size small blob",
			data:        []byte("hello"),
			mediaType:   "text/plain",
			unknownSize: true,
		},
		{
			name:        "unknown size large blob",
			data:        bytes.Repeat([]byte("x"), 1024*1024), // 1MB
			mediaType:   "application/octet-stream",
			unknownSize: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{
				data:        tt.data,
				mediaType:   tt.mediaType,
				unknownSize: tt.unknownSize,
			}

			// Create cached blob
			cached, err := Cache(mock)
			require.NoError(t, err)

			// Test first read
			reader, err := cached.ReadCloser()
			require.NoError(t, err)
			data, err := io.ReadAll(reader)
			require.NoError(t, err)
			assert.Equal(t, tt.data, data)

			// Test second read (should use cache)
			reader, err = cached.ReadCloser()
			require.NoError(t, err)
			data, err = io.ReadAll(reader)
			require.NoError(t, err)
			assert.Equal(t, tt.data, data)
		})
	}
}

func TestCache_Size(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		expected    int64
		unknownSize bool
	}{
		{
			name:     "empty blob",
			data:     []byte{},
			expected: 0,
		},
		{
			name:     "small blob",
			data:     []byte("hello"),
			expected: 5,
		},
		{
			name:     "large blob",
			data:     bytes.Repeat([]byte("x"), 1024*1024), // 1MB
			expected: 1024 * 1024,
		},
		{
			name:        "unknown size empty blob",
			data:        []byte{},
			expected:    0,
			unknownSize: true,
		},
		{
			name:        "unknown size small blob",
			data:        []byte("hello"),
			expected:    int64(len([]byte("hello"))),
			unknownSize: true,
		},
		{
			name:        "unknown size large blob",
			data:        bytes.Repeat([]byte("x"), 1024*1024), // 1MB
			expected:    int64(len(bytes.Repeat([]byte("x"), 1024*1024))),
			unknownSize: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{
				data:        tt.data,
				unknownSize: tt.unknownSize,
			}

			// Create cached blob
			cached, err := Cache(mock)
			require.NoError(t, err)

			// Test size before reading
			assert.Equal(t, tt.expected, cached.Size())

			// Test size after reading
			_ = cached.Data()
			if tt.unknownSize {
				// After reading, size should be known
				assert.Equal(t, int64(len(tt.data)), cached.Size())
			} else {
				assert.Equal(t, tt.expected, cached.Size())
			}
		})
	}
}

func TestCache_Digest(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		expected    string
		unknownSize bool
	}{
		{
			name:     "empty blob",
			data:     []byte{},
			expected: digest.FromBytes([]byte{}).String(),
		},
		{
			name:     "small blob",
			data:     []byte("hello"),
			expected: digest.FromBytes([]byte("hello")).String(),
		},
		{
			name:     "large blob",
			data:     bytes.Repeat([]byte("x"), 1024*1024), // 1MB
			expected: digest.FromBytes(bytes.Repeat([]byte("x"), 1024*1024)).String(),
		},
		{
			name:        "unknown size empty blob",
			data:        []byte{},
			expected:    digest.FromBytes([]byte{}).String(),
			unknownSize: true,
		},
		{
			name:        "unknown size small blob",
			data:        []byte("hello"),
			expected:    digest.FromBytes([]byte("hello")).String(),
			unknownSize: true,
		},
		{
			name:        "unknown size large blob",
			data:        bytes.Repeat([]byte("x"), 1024*1024), // 1MB
			expected:    digest.FromBytes(bytes.Repeat([]byte("x"), 1024*1024)).String(),
			unknownSize: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{
				data:        tt.data,
				unknownSize: tt.unknownSize,
			}

			// Create cached blob
			cached, err := Cache(mock)
			require.NoError(t, err)

			// Test digest before reading
			dig, ok := cached.Digest()
			require.True(t, ok)
			assert.Equal(t, tt.expected, dig)

			// Test digest after reading
			_ = cached.Data()
			require.NoError(t, err)
			dig, ok = cached.Digest()
			require.True(t, ok)
			assert.Equal(t, tt.expected, dig)
		})
	}
}

func TestCache_MediaType(t *testing.T) {
	tests := []struct {
		name       string
		data       []byte
		mediaType  string
		expected   string
		shouldKnow bool
	}{
		{
			name:       "no media type",
			data:       []byte("hello"),
			mediaType:  "",
			expected:   "application/octet-stream",
			shouldKnow: true,
		},
		{
			name:       "with media type",
			data:       []byte("hello"),
			mediaType:  "text/plain",
			expected:   "text/plain",
			shouldKnow: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock blob
			mock := &mockBlob{
				data:      tt.data,
				mediaType: tt.mediaType,
			}

			// Create cached blob
			cached, err := Cache(mock)
			require.NoError(t, err)

			// Test media type
			mt, known := cached.MediaType()
			assert.Equal(t, tt.expected, mt)
			assert.Equal(t, tt.shouldKnow, known)
		})
	}
}

func TestCache_Data(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		mediaType   string
		unknownSize bool
	}{
		{
			name:      "empty blob",
			data:      []byte{},
			mediaType: "",
		},
		{
			name:      "small blob",
			data:      []byte("hello"),
			mediaType: "text/plain",
		},
		{
			name:      "large blob",
			data:      bytes.Repeat([]byte("x"), 1024*1024), // 1MB
			mediaType: "application/octet-stream",
		},
		{
			name:        "unknown size empty blob",
			data:        []byte{},
			mediaType:   "",
			unknownSize: true,
		},
		{
			name:        "unknown size small blob",
			data:        []byte("hello"),
			mediaType:   "text/plain",
			unknownSize: true,
		},
		{
			name:        "unknown size large blob",
			data:        bytes.Repeat([]byte("x"), 1024*1024), // 1MB
			mediaType:   "application/octet-stream",
			unknownSize: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{
				data:        tt.data,
				mediaType:   tt.mediaType,
				unknownSize: tt.unknownSize,
			}

			// Create cached blob
			cached, err := Cache(mock)
			require.NoError(t, err)

			// Test first read
			data := cached.Data()
			assert.Equal(t, tt.data, data)

			// Test second read (should use cache)
			data = cached.Data()
			require.NoError(t, err)
			assert.Equal(t, tt.data, data)
		})
	}
}

func TestCache_ClearCache(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		unknownSize bool
	}{
		{
			name: "known size",
			data: []byte("hello"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{
				data:        tt.data,
				unknownSize: tt.unknownSize,
			}

			// Create cached blob
			cached, err := Cache(mock)
			require.NoError(t, err)

			// Verify data is cached
			assert.Equal(t, int64(len(tt.data)), cached.Size())

			// Verify cache is cleared
			assert.Equal(t, int64(len(tt.data)), cached.Size())
			data := cached.Data()
			require.NotEmpty(t, data)
		})
	}
}

func TestCache_ConcurrentAccess(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		unknownSize bool
	}{
		{
			name: "known size",
			data: bytes.Repeat([]byte("x"), 1024*1024), // 1MB
		},
		{
			name:        "unknown size",
			data:        bytes.Repeat([]byte("x"), 1024*1024), // 1MB
			unknownSize: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{
				data:        tt.data,
				unknownSize: tt.unknownSize,
			}

			// Create cached blob
			cached, err := Cache(mock)
			require.NoError(t, err)

			// Test concurrent access
			done := make(chan struct{})
			for i := 0; i < 10; i++ {
				go func() {
					defer func() { done <- struct{}{} }()

					// Test ReadCloser
					reader, err := cached.ReadCloser()
					require.NoError(t, err)
					_, err = io.ReadAll(reader)
					require.NoError(t, err)

					// Test Data
					data := cached.Data()
					require.NotEmpty(t, data)

					// Test Size
					_ = cached.Size()

					// Test Digest
					_, _ = cached.Digest()

					// Test MediaType
					_, _ = cached.MediaType()
				}()
			}

			// Wait for all goroutines to finish
			for i := 0; i < 10; i++ {
				<-done
			}
		})
	}
}

func TestCache_SizeBeforeRead(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		expected int64
	}{
		{
			name:     "known size before read",
			data:     []byte("test data"),
			expected: 9,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBlob{
				data: tt.data,
			}

			// Create cached blob
			cached, err := Cache(mock)
			require.NoError(t, err)

			// Test size before any read operations
			size := cached.Size()
			assert.Equal(t, tt.expected, size)

			// Verify that the size hasn't changed after multiple calls
			for i := 0; i < 3; i++ {
				assert.Equal(t, tt.expected, cached.Size())
			}

			// Now read the data and verify size changes appropriately
			data := cached.Data()
			require.NotEmpty(t, data)

			assert.Equal(t, tt.expected, cached.Size())
		})
	}
}

func TestCache_Cacheception(t *testing.T) {
	original := inmemory.New(strings.NewReader("test"),
		inmemory.WithMediaType("text/plain"),
		inmemory.WithDigest(digest.SHA512.FromString("test")),
	)
	cache, err := Cache(original)
	require.NoError(t, err)
	assert.NotNil(t, cache)
	assert.Equal(t, "test", string(cache.Data()))
	assert.Equal(t, int64(4), cache.Size())
	dig, ok := cache.Digest()
	require.True(t, ok)
	assert.Equal(t, digest.SHA512.FromString("test").String(), dig)
	mt, ok := cache.MediaType()
	require.True(t, ok)
	assert.Equal(t, "text/plain", mt)
}
