package filesystem_test

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

func TestGetBlobFromPath_SingleFile(t *testing.T) {
	// Setup: create temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Test blob content"

	err := os.WriteFile(testFile, []byte(testContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	opt := filesystem.DirOptions{
		Reproducible: true,
	}

	// Test the exported function
	b, err := filesystem.GetBlobFromPath(context.Background(), testFile, opt)
	if err != nil {
		t.Fatalf("GetBlobFromPath() error = %v", err)
	}

	if b == nil {
		t.Fatal("GetBlobFromPath() returned nil blob")
	}

	// Verify we can read from the blob
	reader, err := b.ReadCloser()
	if err != nil {
		t.Fatalf("Failed to get blob reader: %v", err)
	}
	defer reader.Close()

	// Read TAR content
	tr := tar.NewReader(reader)
	header, err := tr.Next()
	if err != nil {
		t.Fatalf("Failed to read tar header: %v", err)
	}

	if header.Name != "test.txt" {
		t.Errorf("Expected header name 'test.txt', got '%s'", header.Name)
	}

	content, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("Failed to read tar content: %v", err)
	}

	if string(content) != testContent {
		t.Errorf("Expected content '%s', got '%s'", testContent, string(content))
	}
}

func TestGetBlobFromPath_Directory(t *testing.T) {
	// Setup: create temporary test directory with files
	tmpDir := t.TempDir()

	// Create test files
	err := os.WriteFile(filepath.Join(tmpDir, "file1.txt"), []byte("content1"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "file2.txt"), []byte("content2"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	opt := filesystem.DirOptions{
		Reproducible: true,
	}

	// Test the exported function
	b, err := filesystem.GetBlobFromPath(context.Background(), tmpDir, opt)
	if err != nil {
		t.Fatalf("GetBlobFromPath() error = %v", err)
	}

	if b == nil {
		t.Fatal("GetBlobFromPath() returned nil blob")
	}

	// Verify we can read from the blob
	reader, err := b.ReadCloser()
	if err != nil {
		t.Fatalf("Failed to get blob reader: %v", err)
	}
	defer reader.Close()

	// Read TAR content and verify structure
	tr := tar.NewReader(reader)
	fileCount := 0

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Failed to read tar header: %v", err)
		}

		fileCount++
		t.Logf("Found entry in TAR: %s", header.Name)

		// Read content to ensure it's properly written
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("Failed to read content for %s: %v", header.Name, err)
		}

		// Verify content matches expected
		if strings.Contains(header.Name, "file1.txt") && string(content) != "content1" {
			t.Errorf("Expected content1 for file1.txt, got %s", string(content))
		}
		if strings.Contains(header.Name, "file2.txt") && string(content) != "content2" {
			t.Errorf("Expected content2 for file2.txt, got %s", string(content))
		}
	}

	if fileCount == 0 {
		t.Error("Expected at least one file in TAR archive, got none")
	}
}
