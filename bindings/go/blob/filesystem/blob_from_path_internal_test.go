package filesystem

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tests for internal functions (development only).
func TestCreateTarHeader_Internal(t *testing.T) {
	tests := []struct {
		name           string
		reproducible   bool
		wantNormalized bool
	}{
		{
			name:           "normal header",
			reproducible:   false,
			wantNormalized: false,
		},
		{
			name:           "reproducible header",
			reproducible:   true,
			wantNormalized: true,
		},
	}

	// Create a temporary file to obtain FileInfo
	tmpFile, err := os.CreateTemp("", "test_*.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	fi, err := tmpFile.Stat()
	if err != nil {
		t.Fatal(err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header, err := createTarHeader(fi, "", tt.reproducible)
			if err != nil {
				t.Fatalf("createTarHeader() error = %v", err)
			}

			if header == nil {
				t.Fatal("createTarHeader() returned nil header")
			}

			if tt.wantNormalized {
				if !header.ModTime.Equal(time.Unix(0, 0)) {
					t.Errorf("expected normalized ModTime, got %v", header.ModTime)
				}
				if header.Uid != 0 || header.Gid != 0 {
					t.Errorf("expected normalized Uid/Gid to be 0, got Uid=%d, Gid=%d", header.Uid, header.Gid)
				}
			}
		})
	}
}

func TestCreateTarFromSingleFile_Internal(t *testing.T) {
	// Setup: create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello, World!"

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Create filesystem rooted at tmpDir
	fs, err := NewFS(tmpDir, os.O_RDONLY)
	if err != nil {
		t.Fatal(err)
	}

	// Capture TAR output
	var tarData strings.Builder
	tw := tar.NewWriter(&tarData)

	opt := &DirOptions{
		Reproducible: true,
	}

	// Execute function under test
	err = createTarFromSingleFile(context.Background(), fs, "test.txt", opt, tw)
	if err != nil {
		t.Fatalf("createTarFromSingleFile() error = %v", err)
	}

	err = tw.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Verify TAR content
	tr := tar.NewReader(strings.NewReader(tarData.String()))
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("failed to read tar header: %v", err)
	}

	if header.Name != "test.txt" {
		t.Errorf("expected header name 'test.txt', got '%s'", header.Name)
	}

	content, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("failed to read tar content: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("expected content '%s', got '%s'", testContent, string(content))
	}
}

func TestCreateTarStream_Internal(t *testing.T) {
	// Setup: create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Test file content"

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	opt := &DirOptions{
		Reproducible: true,
	}

	// Execute stream creator and verify
	reader, err := createTarStream(context.Background(), testFile, opt)
	if err != nil {
		t.Fatalf("createTarStream() error = %v", err)
	}

	tr := tar.NewReader(reader)
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("failed to read tar header: %v", err)
	}

	if header.Name != "test.txt" {
		t.Errorf("expected header name 'test.txt', got '%s'", header.Name)
	}

	content, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("failed to read tar content: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("expected content '%s', got '%s'", testContent, string(content))
	}
}
