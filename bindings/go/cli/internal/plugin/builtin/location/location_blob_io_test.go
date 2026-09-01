package location

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func TestWriteAndRead(t *testing.T) {
	r := require.New(t)

	// Create a temporary directory for test files
	tempDir := t.TempDir()

	// Test data
	testData := []byte("hello world!")

	// Test cases
	tests := []struct {
		name        string
		location    types.Location
		setup       func(t *testing.T) error
		cleanup     func() error
		expectError bool
		skipOnOS    string
		asyncWrite  bool
	}{
		{
			name: "write and read from local file",
			location: types.Location{
				LocationType: types.LocationTypeLocalFile,
				Value:        filepath.Join(tempDir, "test.txt"),
			},
			setup: func(t *testing.T) error {
				return nil // No setup needed
			},
			cleanup: func() error {
				return os.Remove(filepath.Join(tempDir, "test.txt"))
			},
			expectError: false,
		},
		{
			name: "write and read from named pipe",
			location: types.Location{
				LocationType: types.LocationTypeUnixNamedPipe,
				Value:        filepath.Join(tempDir, "pipe"),
			},
			setup: func(t *testing.T) error {
				return unix.Mkfifo(filepath.Join(tempDir, "pipe"), 0666)
			},
			cleanup: func() error {
				return os.Remove(filepath.Join(tempDir, "pipe"))
			},
			expectError: false,
			skipOnOS:    "windows",
			asyncWrite:  true,
		},
		{
			name: "unsupported location type",
			location: types.Location{
				LocationType: "unsupported",
				Value:        "test.txt",
			},
			setup:       func(t *testing.T) error { return nil },
			cleanup:     func() error { return nil },
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipOnOS != "" && runtime.GOOS == tt.skipOnOS {
				t.Skip("skipping test on", runtime.GOOS)
			}

			// Setup
			if err := tt.setup(t); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
			defer tt.cleanup()

			// Create a test blob
			testBlob := blob.NewDirectReadOnlyBlob(bytes.NewReader(testData))

			write := func() {
				err := Write(tt.location, testBlob)
				if tt.expectError {
					r.Error(err, "expected error but got none")
					return
				}
				r.NoError(err, "unexpected error while writing blob")
			}

			// Test Write
			if tt.asyncWrite {
				go write()
			} else {
				write()
			}

			// Test Read
			readBlob, err := Read(tt.location)
			if tt.expectError {
				r.Error(err, "expected error but got none")
				return
			} else {
				r.NoError(err, "unexpected error while reading blob")
			}

			// Verify the content
			reader, err := readBlob.ReadCloser()
			r.NoError(err, "unexpected error getting reader from blob")
			defer reader.Close()

			readData, err := io.ReadAll(reader)
			r.NoError(err, "unexpected error reading blob data")
			r.Equal(testData, readData, "blob content mismatch")
		})
	}
}

func TestWriteErrors(t *testing.T) {
	r := require.New(t)

	// Test cases for write errors
	tests := []struct {
		name        string
		location    types.Location
		blob        blob.ReadOnlyBlob
		expectError bool
	}{
		{
			name: "write to non-existent directory",
			location: types.Location{
				LocationType: types.LocationTypeLocalFile,
				Value:        "/non/existent/path/test.txt",
			},
			blob:        blob.NewDirectReadOnlyBlob(bytes.NewReader([]byte("test"))),
			expectError: true,
		},
		{
			name: "write to invalid named pipe",
			location: types.Location{
				LocationType: types.LocationTypeUnixNamedPipe,
				Value:        "/non/existent/path/test.pipe",
			},
			blob:        blob.NewDirectReadOnlyBlob(bytes.NewReader([]byte("test"))),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Write(tt.location, tt.blob)
			if tt.expectError {
				r.Error(err, "expected error but got none")
			} else {
				r.NoError(err, "unexpected error while writing blob")
			}
		})
	}
}

func TestReadErrors(t *testing.T) {
	r := require.New(t)

	// Test cases for read errors
	tests := []struct {
		name        string
		location    types.Location
		expectError bool
	}{
		{
			name: "read from non-existent file",
			location: types.Location{
				LocationType: types.LocationTypeLocalFile,
				Value:        "/non/existent/path/test.txt",
			},
			expectError: true,
		},
		{
			name: "read from non-existent named pipe",
			location: types.Location{
				LocationType: types.LocationTypeUnixNamedPipe,
				Value:        "/non/existent/path/test.pipe",
			},
			expectError: true,
		},
		{
			name: "read with unsupported location type",
			location: types.Location{
				LocationType: "unsupported",
				Value:        "test.txt",
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Read(tt.location)
			if tt.expectError {
				r.Error(err, "expected error but got none")
			} else {
				r.NoError(err, "unexpected error while reading blob")
			}
		})
	}
}
