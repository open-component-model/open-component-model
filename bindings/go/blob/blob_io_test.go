package blob_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

func TestCopy_DirectBlob(t *testing.T) {
	r := require.New(t)
	blobData := []byte("hello world!")
	directBlob := blob.NewDirectReadOnlyBlob(bytes.NewReader(blobData))
	var buf bytes.Buffer

	err := blob.Copy(&buf, directBlob)
	r.NoError(err, "unexpected error while copying blob")
	r.Equal(blobData, buf.Bytes(), "blob content mismatch")
}

func TestCopy_File(t *testing.T) {
	r := require.New(t)
	tmp := filepath.Join(os.TempDir(), "blob-test")
	blobData := []byte("hello world!")
	directBlob := blob.NewDirectReadOnlyBlob(bytes.NewReader(blobData))

	f, err := os.Create(tmp)
	r.NoError(err, "unexpected error while creating temp file")
	t.Cleanup(func() {
		r.NoError(f.Close())
	})

	err = blob.Copy(f, directBlob)
	r.NoError(err, "unexpected error while copying blob")

	data, err := os.ReadFile(tmp)
	r.NoError(err, "unexpected error while reading temp file")
	r.Equal(blobData, data, "blob content mismatch")
}

func TestCopy_DirectBlob_ReadError(t *testing.T) {
	r := require.New(t)
	errorReader := &errorReader{}
	directBlob := blob.NewDirectReadOnlyBlob(errorReader)
	var buf bytes.Buffer

	err := blob.Copy(&buf, directBlob)
	r.Error(err, "expected error, got nil")
	r.Contains(err.Error(), "mock read error", "unexpected error message")
}

type errorReader struct{}

func (e *errorReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("mock read error")
}

// MockReadOnlyBlob is a mock implementation of ReadOnlyBlob
type MockReadOnlyBlob struct {
	mock.Mock
}

func (m *MockReadOnlyBlob) ReadCloser() (io.ReadCloser, error) {
	args := m.Called()
	rc := args.Get(0)
	if rc == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

func (m *MockReadOnlyBlob) Size() int64 {
	args := m.Called()
	return args.Get(0).(int64)
}

func (m *MockReadOnlyBlob) Digest() (string, bool) {
	args := m.Called()
	return args.String(0), args.Bool(1)
}

// TestCopy tests the Copy function
func TestCopy(t *testing.T) {
	tests := []struct {
		name          string
		mockBlob      func() *MockReadOnlyBlob
		expectedError error
	}{
		{
			name: "successful copy with size aware",
			mockBlob: func() *MockReadOnlyBlob {
				m := new(MockReadOnlyBlob)
				m.On("ReadCloser").Return(io.NopCloser(bytes.NewReader([]byte("test data"))), nil)
				m.On("Size").Return(int64(9))
				m.On("Digest").Return("", false)
				return m
			},
			expectedError: nil,
		},
		{
			name: "error on read",
			mockBlob: func() *MockReadOnlyBlob {
				m := new(MockReadOnlyBlob)
				m.On("ReadCloser").Return(nil, errors.New("read error"))
				m.On("Size").Return(int64(9))
				m.On("Digest").Return("", false)
				return m
			},
			expectedError: errors.New("read error"),
		},
		{
			name: "digest format error (not a container digest)",
			mockBlob: func() *MockReadOnlyBlob {
				m := new(MockReadOnlyBlob)
				data := []byte("test data")
				m.On("ReadCloser").Return(io.NopCloser(bytes.NewReader(data)), nil)
				m.On("Size").Return(int64(len(data)))
				m.On("Digest").Return("invlaid", true)
				return m
			},
			expectedError: errors.New("invalid checksum digest format"),
		},
		{
			name: "digest verification failure",
			mockBlob: func() *MockReadOnlyBlob {
				m := new(MockReadOnlyBlob)
				data := []byte("test data")
				m.On("ReadCloser").Return(io.NopCloser(bytes.NewReader(data)), nil)
				m.On("Size").Return(int64(len(data)))
				m.On("Digest").Return(digest.FromBytes([]byte("other")).String(), true)
				return m
			},
			expectedError: errors.New("blob digest verification failed"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := new(bytes.Buffer)
			src := tt.mockBlob()

			err := blob.Copy(dst, src)

			if tt.expectedError != nil {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, "test data", dst.String())
			}
		})
	}
}
