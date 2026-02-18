package transformer

import (
	"fmt"
	"os"
	"path/filepath"
)

// DetermineOutputPath determines the output path for buffering the blob content.
// If the outputPath is empty, it creates a temporary file.
// If the outputPath is provided, it ensures that the directory exists.
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
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("failed creating output directory: %w", err)
		}
	}
	return outputPath, nil
}
