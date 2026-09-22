package filesystem_test

import (
	"archive/tar"
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// CORE FUNCTIONALITY: Basic single file and directory blob creation
func TestGetBlobFromPath_SingleFile(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testContent := "Test blob content"
	testFile := createTestFile(t, tmpDir, "test.txt", testContent)

	// Test: create blob from single file (should be raw, not TAR)
	opt := filesystem.DirOptions{Reproducible: true}
	b, err := filesystem.GetBlobFromPath(t.Context(), testFile, opt)
	r.NoError(err)
	r.NotNil(b)

	// Verify: blob contains raw file content (not TAR archive)
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	content, err := io.ReadAll(reader)
	r.NoError(err)
	r.Equal(testContent, string(content))
}

func TestGetBlobFromPath_SimpleDirectory(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with multiple files
	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "file1.txt", "content1")
	createTestFile(t, tmpDir, "file2.txt", "content2")

	// Test: create blob from directory (should be TAR archive)
	opt := filesystem.DirOptions{Reproducible: true}
	b, err := filesystem.GetBlobFromPath(t.Context(), tmpDir, opt)
	r.NoError(err)
	r.NotNil(b)

	// Verify: directory structure is preserved in TAR
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	foundFiles := map[string]string{}

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		content, err := io.ReadAll(tr)
		r.NoError(err)

		name := header.Name
		foundFiles[name] = string(content)
	}

	r.Equal("content1", foundFiles["file1.txt"])
	r.Equal("content2", foundFiles["file2.txt"])
}

func TestGetBlobFromPath_RelativeDirectory(t *testing.T) {
	r := require.New(t)

	// Change working directory to explicitly not be in the working directory we pass via options
	wd := t.TempDir()
	t.Chdir(wd)

	// Setup: create directory with multiple files
	tmpDir := t.TempDir()
	createTestFile(t, filepath.Join(tmpDir, "testFolder"), "file.txt", "content")

	// Test: create blob from directory (should be TAR archive)
	opt := filesystem.DirOptions{Reproducible: true}
	opt.WorkingDir = tmpDir
	b, err := filesystem.GetBlobFromPath(t.Context(), "testFolder", opt)
	r.NoError(err)
	r.NotNil(b)

	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()
}

// PATTERN FILTERING: Include/exclude pattern functionality
func TestGetBlobFromPath_PatternSemantics(t *testing.T) {
	r := require.New(t)

	tests := []struct {
		name            string
		includePatterns []string
		excludePatterns []string
		expectedFiles   []string
		expectError     bool
	}{
		{
			name:            "Include only go files, exclude test files",
			includePatterns: []string{"*.go"},
			excludePatterns: []string{"*_test.go"},
			expectedFiles:   []string{"main.go", "helper.go"},
		},
		{
			name:            "Include files in subdirectory",
			includePatterns: []string{"config/my-config.json", "*.md"},
			excludePatterns: []string{},
			expectedFiles:   []string{"config/my-config.json", "README.md"},
		},
		{
			name:            "Exclude test directory and tmp files",
			includePatterns: []string{},
			excludePatterns: []string{"test", "*.tmp"},
			expectedFiles:   []string{"main.go", "helper.go", "main_test.go", "config/my-config.json", "README.md"},
		},
		{
			name:            "Combine includes and excludes",
			includePatterns: []string{"*.go", "config/my-config.json"},
			excludePatterns: []string{"*_test.go"},
			expectedFiles:   []string{"main.go", "helper.go", "config/my-config.json"},
		},
		{
			name:            "Exclude precedence over include",
			includePatterns: []string{"*.go"},
			excludePatterns: []string{"main.go"},
			expectedFiles:   []string{"helper.go", "main_test.go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			tmpDir := t.TempDir()
			createTestFile(t, tmpDir, "main.go", "package main")
			createTestFile(t, tmpDir, "helper.go", "package helper")
			createTestFile(t, tmpDir, "main_test.go", "package main")
			createTestFile(t, tmpDir, "config/my-config.json", `{"key": "value"}`)
			createTestFile(t, tmpDir, "README.md", "# Project")
			createTestFile(t, tmpDir, "temp.tmp", "temporary")
			require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "test"), 0755))
			createTestFile(t, tmpDir, "test/file.txt", "test content")

			// Test with patterns
			opt := filesystem.DirOptions{IncludePatterns: tt.includePatterns, ExcludePatterns: tt.excludePatterns, Reproducible: true}
			resultBlob, err := filesystem.GetBlobFromPath(t.Context(), tmpDir, opt)
			if tt.expectError {
				r.Error(err)
				return
			}
			r.NoError(err)
			r.NotNil(resultBlob)

			// Verify TAR contents
			files := extractTarContents(t, resultBlob)
			r.ElementsMatch(tt.expectedFiles, files)
		})
	}
}

func TestGetBlobFromPath_SingleFileWithPatterns(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()
	testFile := createTestFile(t, tmpDir, "test.txt", "content")

	// Test: patterns with single file should error
	opt := filesystem.DirOptions{IncludePatterns: []string{"*.txt"}}
	_, err := filesystem.GetBlobFromPath(t.Context(), testFile, opt)
	r.Error(err)
	r.Contains(err.Error(), "include/exclude patterns are not supported for single files")

	// Test: exclude patterns with single file should also error
	opt = filesystem.DirOptions{ExcludePatterns: []string{"*.log"}}
	_, err = filesystem.GetBlobFromPath(t.Context(), testFile, opt)
	r.Error(err)
	r.Contains(err.Error(), "include/exclude patterns are not supported for single files")
}

// DIRECTORY STRUCTURE OPTIONS
func TestGetBlobFromPath_PreserveDirectory(t *testing.T) {
	r := require.New(t)

	// Setup: create named directory with content
	parent := t.TempDir()
	targetDirName := "preserve_me"
	targetDir := filepath.Join(parent, targetDirName)
	r.NoError(os.Mkdir(targetDir, 0755))
	createTestFile(t, targetDir, "file.txt", "content")

	// Test: preserve directory structure
	opt := filesystem.DirOptions{Reproducible: true, PreserveDir: true}
	b, err := filesystem.GetBlobFromPath(t.Context(), targetDir, opt)
	r.NoError(err)

	// Verify: entries are prefixed with directory name
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	foundPrefixed := false
	var foundHeaders []string
	expectedDirHeader := filepath.Base(targetDir) + "/"

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		foundHeaders = append(foundHeaders, header.Name)

		// Expect exact directory header for the preserved directory
		if header.Typeflag == tar.TypeDir && header.Name == expectedDirHeader {
			foundPrefixed = true
		}

		// Consume content
		_, err = io.ReadAll(tr)
		r.NoError(err)
	}

	// Debug output to understand what we got
	if !foundPrefixed {
		t.Logf("Expected entry: %s", targetDirName)
		t.Logf("Found headers: %v", foundHeaders)
	}

	r.True(foundPrefixed, "expected entries to be prefixed with directory name when PreserveDir=true")
}

// COMPRESSION AND MEDIA TYPE
func TestGetBlobFromPath_Compression(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := createTestFile(t, tmpDir, "test.txt", "test content")

	// Test: compression enabled
	opt := filesystem.DirOptions{Compress: true, MediaType: filesystem.DefaultTarMediaType}
	b, err := filesystem.GetBlobFromPath(t.Context(), testFile, opt)
	r.NoError(err)

	// Verify: content is gzip compressed
	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	// Check gzip magic bytes
	magicBytes := make([]byte, 2)
	_, err = io.ReadFull(reader, magicBytes)
	r.NoError(err)
	r.Equal(byte(0x1f), magicBytes[0])
	r.Equal(byte(0x8b), magicBytes[1])
}

func TestGetBlobFromPath_MediaTypeHandling(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()

	t.Run("Directory with custom media type", func(t *testing.T) {
		createTestFile(t, tmpDir, "file.txt", "content")

		opt := filesystem.DirOptions{MediaType: "application/custom-tar"}
		b, err := filesystem.GetBlobFromPath(t.Context(), tmpDir, opt)
		r.NoError(err)

		mt, ok := b.(blob.MediaTypeAware)
		r.True(ok)
		media, known := mt.MediaType()
		r.True(known)
		r.Equal("application/custom-tar", media)
	})

	t.Run("Single file with custom media type", func(t *testing.T) {
		testFile := createTestFile(t, tmpDir, "single.txt", "content")

		opt := filesystem.DirOptions{MediaType: "text/plain"}
		b, err := filesystem.GetBlobFromPath(t.Context(), testFile, opt)
		r.NoError(err)

		mt, ok := b.(blob.MediaTypeAware)
		r.True(ok)
		media, known := mt.MediaType()
		r.True(known)
		r.Equal("text/plain", media)
	})

	// A declared media type is used as-is when compression is enabled; only
	// the default media type gets a +gzip suffix.
	t.Run("Directory with custom media type and compression", func(t *testing.T) {
		createTestFile(t, tmpDir, "file2.txt", "content")

		opt := filesystem.DirOptions{MediaType: "application/custom+tar", Compress: true}
		b, err := filesystem.GetBlobFromPath(t.Context(), tmpDir, opt)
		r.NoError(err)

		mt, ok := b.(blob.MediaTypeAware)
		r.True(ok)
		media, known := mt.MediaType()
		r.True(known)
		r.Equal("application/custom+tar", media)
	})

	t.Run("Single file with custom media type and compression", func(t *testing.T) {
		testFile := createTestFile(t, tmpDir, "single2.txt", "content")

		opt := filesystem.DirOptions{MediaType: "text/plain", Compress: true}
		b, err := filesystem.GetBlobFromPath(t.Context(), testFile, opt)
		r.NoError(err)

		mt, ok := b.(blob.MediaTypeAware)
		r.True(ok)
		media, known := mt.MediaType()
		r.True(known)
		r.Equal("text/plain", media)
	})
}

// REPRODUCIBILITY
func TestGetBlobFromPath_ReproducibleBuilds(t *testing.T) {
	r := require.New(t)

	// Setup: create test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")
	r.NoError(os.WriteFile(testFile, []byte("test content"), 0644))

	// Test: create blob with reproducible option
	opt := filesystem.DirOptions{Reproducible: true}
	blob1, err := filesystem.GetBlobFromPath(t.Context(), testFile, opt)
	r.NoError(err)

	data1, err := readAllFromBlob(blob1)
	r.NoError(err)

	// Modify file timestamp
	newTime := time.Now().Add(5 * time.Minute)
	r.NoError(os.Chtimes(testFile, newTime, newTime))

	// Test: create blob again after timestamp change
	blob2, err := filesystem.GetBlobFromPath(t.Context(), testFile, opt)
	r.NoError(err)

	data2, err := readAllFromBlob(blob2)
	r.NoError(err)

	// Verify: reproducible builds produce identical output
	r.Equal(data1, data2, "expected reproducible builds to produce identical output")
}

// ERROR HANDLING & EDGE CASES
func TestGetBlobFromPath_ErrorCases(t *testing.T) {
	tests := []struct {
		name        string
		setupFunc   func(t *testing.T) (string, filesystem.DirOptions)
		expectError bool
		errorText   string
	}{
		{
			name: "empty_path",
			setupFunc: func(t *testing.T) (string, filesystem.DirOptions) {
				return "", filesystem.DirOptions{}
			},
			expectError: true,
		},
		{
			name: "non_existent_path",
			setupFunc: func(t *testing.T) (string, filesystem.DirOptions) {
				return "/non/existent/path", filesystem.DirOptions{}
			},
			expectError: true,
		},
		{
			name: "path_outside_working_directory",
			setupFunc: func(t *testing.T) (string, filesystem.DirOptions) {
				base := t.TempDir()
				allowed := filepath.Join(base, "allowed")
				outside := filepath.Join(base, "outside")
				require.NoError(t, os.MkdirAll(allowed, 0755))
				require.NoError(t, os.MkdirAll(outside, 0755))

				testFile := filepath.Join(outside, "test.txt")
				require.NoError(t, os.WriteFile(testFile, []byte("content"), 0644))

				return testFile, filesystem.DirOptions{WorkingDir: allowed}
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			path, opt := tt.setupFunc(t)
			_, err := filesystem.GetBlobFromPath(t.Context(), path, opt)

			if tt.expectError {
				r.Error(err)
				if tt.errorText != "" {
					r.Contains(err.Error(), tt.errorText)
				}
			} else {
				r.NoError(err)
			}
		})
	}
}

func TestGetBlobFromPath_SymlinkRejection(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with symlink
	tmpDir := t.TempDir()
	targetFile := filepath.Join(tmpDir, "target.txt")
	symlinkFile := filepath.Join(tmpDir, "symlink.txt")

	r.NoError(os.WriteFile(targetFile, []byte("target content"), 0644))

	if err := os.Symlink("target.txt", symlinkFile); err != nil {
		t.Skipf("symlink creation failed (may not be supported on this system): %v", err)
		return
	}

	// Test: symlinks should be rejected during blob reading
	b, err := filesystem.GetBlobFromPath(t.Context(), tmpDir, filesystem.DirOptions{})
	r.NoError(err, "blob creation should succeed initially")

	// Verify: error occurs when reading blob content
	_, err = readAllFromBlob(b)
	r.Error(err)
	r.Contains(err.Error(), "symlinks are not supported")
}

func TestGetBlobFromPath_IncludeDirectoryOnly(t *testing.T) {
	r := require.New(t)

	// Setup: create directory with an empty sub directory
	tmpDir := t.TempDir()
	targetDir := filepath.Join(tmpDir, "sub", "dir")
	r.NoError(os.MkdirAll(targetDir, 0755))

	// Only include the directory itself
	opt := filesystem.DirOptions{IncludePatterns: []string{"sub/dir"}, Reproducible: true}
	b, err := filesystem.GetBlobFromPath(t.Context(), tmpDir, opt)
	r.NoError(err)
	r.NotNil(b)

	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	tr := tar.NewReader(reader)
	foundDir := false
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		if h.Typeflag == tar.TypeDir && h.Name == "sub/dir/" {
			foundDir = true
		}
		_, err = io.ReadAll(tr)
		r.NoError(err)
	}
	r.True(foundDir, "expected directory header for sub/dir to be present when included explicitly")
}

func TestGetBlobFromPath_PatternNormalization(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "sub/dir/file.txt", "content")

	cases := [][]string{
		{"./sub/dir/*"},
		{"/sub/dir/*"},
		{"sub/dir/*"},
	}

	for _, inc := range cases {
		opt := filesystem.DirOptions{IncludePatterns: inc, Reproducible: true}
		resultBlob, err := filesystem.GetBlobFromPath(t.Context(), tmpDir, opt)
		r.NoError(err)
		files := extractTarContents(t, resultBlob)
		r.Contains(files, "sub/dir/file.txt")
	}
}

// HELPERS
func readAllFromBlob(b blob.ReadOnlyBlob) ([]byte, error) {
	rc, err := b.ReadCloser()
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(rc)
	closeErr := rc.Close()

	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}

func createTestFile(t *testing.T, basePath, relativePath, content string) string {
	fullPath := filepath.Join(basePath, relativePath)
	dir := filepath.Dir(fullPath)

	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(fullPath, []byte(content), 0644))

	return fullPath
}

func extractTarContents(t *testing.T, b blob.ReadOnlyBlob) []string {
	reader, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { require.NoError(t, reader.Close()) }()

	tr := tar.NewReader(reader)
	var files []string

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		// Only track regular files, not directories
		if header.Typeflag == tar.TypeReg {
			name := header.Name
			files = append(files, name)
		}

		// Consume content
		_, err = io.ReadAll(tr)
		require.NoError(t, err)
	}

	return files
}

// Git-style directory layout is explicitly opt-in.
func TestGetBlobFromPath_ArchiveLayout(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()
	r.NoError(os.MkdirAll(filepath.Join(tmpDir, "sub", "nested"), 0755))
	createTestFile(t, tmpDir, "root.txt", "root")
	createTestFile(t, filepath.Join(tmpDir, "sub"), "file.txt", "content")

	b, err := filesystem.GetBlobFromPath(t.Context(), tmpDir, filesystem.DirOptions{
		Reproducible: true, OmitRoot: true, OmitDirTrailingSlash: true,
	})
	r.NoError(err)

	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	names := map[string]byte{}
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		names[header.Name] = header.Typeflag

		_, err = io.ReadAll(tr)
		r.NoError(err)
	}

	r.Equal(map[string]byte{
		"root.txt":     tar.TypeReg,
		"sub":          tar.TypeDir,
		"sub/file.txt": tar.TypeReg,
		"sub/nested":   tar.TypeDir,
	}, names)
}

// PreserveSymlinks stores a link as a link. The target is recorded as written,
// so an absolute or dangling one is kept verbatim rather than resolved, and the
// walk does not descend through a link to a directory.
func TestGetBlobFromPath_PreserveSymlinks(t *testing.T) {
	r := require.New(t)

	tmpDir := t.TempDir()
	createTestFile(t, tmpDir, "target.txt", "target content")
	r.NoError(os.MkdirAll(filepath.Join(tmpDir, "realdir"), 0755))
	createTestFile(t, filepath.Join(tmpDir, "realdir"), "inner.txt", "inner")

	links := map[string]string{
		"relative.txt": "target.txt",
		"absolute.txt": "/etc/hosts",
		"dangling.txt": "nonexistent.txt",
		"escaping.txt": "../../outside.txt",
		"dirlink":      "realdir",
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(tmpDir, name)); err != nil {
			t.Skipf("symlink creation failed (may not be supported on this system): %v", err)
			return
		}
	}

	b, err := filesystem.GetBlobFromPath(t.Context(), tmpDir, filesystem.DirOptions{PreserveSymlinks: true})
	r.NoError(err)

	reader, err := b.ReadCloser()
	r.NoError(err)
	defer func() { r.NoError(reader.Close()) }()

	targets := map[string]string{}
	var names []string
	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)

		names = append(names, header.Name)
		if header.Typeflag == tar.TypeSymlink {
			targets[header.Name] = header.Linkname
		}
		_, err = io.ReadAll(tr)
		r.NoError(err)
	}

	r.Equal(links, targets, "every link is stored as a link, with its target as written")

	// The link to a directory contributes the link alone; the directory itself is
	// still walked under its own name.
	r.NotContains(names, "dirlink/inner.txt")
	r.Contains(names, "realdir/inner.txt")
}

func TestGetBlobFromPath_DefaultTarBytes(t *testing.T) {
	for _, reproducible := range []bool{false, true} {
		name := "default"
		if reproducible {
			name = "reproducible"
		}
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			r.NoError(os.Mkdir(filepath.Join(dir, "sub"), 0o755))
			r.NoError(os.WriteFile(filepath.Join(dir, "sub", "file"), []byte("content"), 0o644))
			var expected bytes.Buffer
			tw := tar.NewWriter(&expected)
			for _, name := range []string{".", "sub", "sub/file"} {
				info, err := os.Stat(filepath.Join(dir, name))
				r.NoError(err)
				h, err := tar.FileInfoHeader(info, "")
				r.NoError(err)
				h.Name = name
				if info.IsDir() {
					h.Name += "/"
				}
				if reproducible {
					h.ModTime, h.AccessTime, h.ChangeTime = time.Unix(0, 0), time.Unix(0, 0), time.Unix(0, 0)
					h.Uid, h.Gid, h.Uname, h.Gname = 0, 0, "", ""
					h.Mode &= 0o777
				}
				r.NoError(tw.WriteHeader(h))
				if !info.IsDir() {
					_, err = tw.Write([]byte("content"))
					r.NoError(err)
				}
			}
			r.NoError(tw.Close())
			b, err := filesystem.GetBlobFromPath(t.Context(), dir, filesystem.DirOptions{Reproducible: reproducible})
			r.NoError(err)
			actual, err := readAllFromBlob(b)
			r.NoError(err)
			r.Equal(expected.Bytes(), actual, "default bytes retain ./ and sub/ entries")
		})
	}
}

func TestWriteTarEntryLayoutOptions(t *testing.T) {
	for _, tt := range []struct {
		name  string
		opt   filesystem.DirOptions
		names []string
	}{
		{name: "defaults", names: []string{"./", "sub/"}},
		{name: "omit root", opt: filesystem.DirOptions{OmitRoot: true}, names: []string{"sub/"}},
		{name: "omit slash", opt: filesystem.DirOptions{OmitDirTrailingSlash: true}, names: []string{".", "sub"}},
		{name: "git layout", opt: filesystem.DirOptions{OmitRoot: true, OmitDirTrailingSlash: true}, names: []string{"sub"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			source := fstest.MapFS{"sub": &fstest.MapFile{Mode: fs.ModeDir | 0o755}}
			var output bytes.Buffer
			writer := tar.NewWriter(&output)
			for _, name := range []string{".", "sub"} {
				info, err := fs.Stat(source, name)
				r.NoError(err)
				r.NoError(filesystem.WriteTarEntry(t.Context(), name, info, source, tt.opt, writer))
			}
			r.NoError(writer.Close())
			reader := tar.NewReader(&output)
			var names []string
			for range tt.names {
				h, err := reader.Next()
				r.NoError(err)
				names = append(names, h.Name)
			}
			r.Equal(tt.names, names)
			_, err := reader.Next()
			r.ErrorIs(err, io.EOF)
		})
	}
}
