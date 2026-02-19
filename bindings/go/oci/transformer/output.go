package transformer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DetermineOutputPath determines the output path for buffering the blob content.
// If the outputPath is empty, it creates a temporary file with an appropriate extension based on the media type of the blob content.
// If the outputPath is provided, it ensures that the directory exists, and we can write to the file.
// If the outputPath is a directory, it creates a temporary file in that directory with the filePrefix as a prefix.
// If the outputPath is a file path, it returns that path directly, ignoring the filePrefix.
func DetermineOutputPath(outputPath string, filePrefix string) (string, error) {
	if outputPath == "" {
		// Create a temporary file
		tempFile, err := os.CreateTemp("", filePrefix+"-*")
		if err != nil {
			return "", fmt.Errorf("failed creating temporary file: %w", err)
		}
		_ = tempFile.Close() // Close immediately, BlobToSpec will overwrite it
		outputPath = tempFile.Name()
	} else {
		// Ensure the directory exists
		dir := filepath.Dir(outputPath)

		// create the directory if it does not exist
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("failed creating output directory: %w", err)
		}

		// check if outputPath contains a file name, if not create a file based on the prefix
		info, err := os.Stat(outputPath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("failed accessing output path: %w", err)
		} else if err == nil && info.IsDir() {
			tmpFile, err := os.CreateTemp(outputPath, filePrefix+"-*")
			if err != nil {
				return "", fmt.Errorf("failed creating temporary file in output directory: %w", err)
			}
			_ = tmpFile.Close()
			outputPath = tmpFile.Name()
		}
	}
	return outputPath, nil
}
