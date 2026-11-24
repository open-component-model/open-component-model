package filesystem

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// BufferFile creates an empty file for a caller to buffer content in and returns its
// absolute path, so that the path stays valid regardless of the working directory of
// whoever reads it later.
//
// The file is created in outputPath when that is set, otherwise in tempDir, which is
// created when it does not exist yet, and in the temporary directory of the operating
// system when neither is set. filePrefix prefixes the generated file name.
//
// An outputPath that does not exist or is not a directory is an error.
//
// The caller owns the returned file and removes it when buffering fails.
func BufferFile(outputPath, tempDir, filePrefix string) (string, error) {
	dir := outputPath
	switch {
	case outputPath != "":
		info, err := os.Stat(outputPath)
		if err != nil {
			return "", fmt.Errorf("output path does not exist: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("output path %q is a file, not a directory", outputPath)
		}
	case tempDir != "":
		if err := os.MkdirAll(tempDir, 0o700); err != nil {
			return "", fmt.Errorf("failed to create temporary directory: %w", err)
		}
		dir = tempDir
	}

	file, err := os.CreateTemp(dir, filePrefix+"-*")
	if err != nil {
		return "", fmt.Errorf("failed creating temporary file: %w", err)
	}
	// Closed immediately, the caller writes the content. An open file can neither be
	// removed nor have its directory removed on windows.
	if err := file.Close(); err != nil {
		return "", errors.Join(fmt.Errorf("failed closing temporary file: %w", err), os.Remove(file.Name()))
	}

	absPath, err := filepath.Abs(file.Name())
	if err != nil {
		return "", errors.Join(fmt.Errorf("cannot resolve absolute path: %w", err), os.Remove(file.Name()))
	}
	return absPath, nil
}
