package transformer

import (
	"fmt"
	"os"
)

// DetermineOutputPath determines the output path for buffering the blob content.
// If the outputPath is empty, it creates a temporary file in the default temp directory.
// If the outputPath is provided, it checks if the path exists:
//   - If the outputPath does not exist, it returns an error.
//   - If the outputPath exists and is a file, it returns an error.
//   - If the outputPath exists and is a directory, it creates a temporary file in that directory with the filePrefix as a prefix.
func DetermineOutputPath(outputPath string, filePrefix string) (string, error) {
	if outputPath == "" {
		// Create a temporary file in the default temp directory
		tempFile, err := os.CreateTemp("", filePrefix+"-*")
		if err != nil {
			return "", fmt.Errorf("failed creating temporary file: %w", err)
		}
		defer func() {
			_ = tempFile.Close()
		}()
		return tempFile.Name(), nil
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return "", fmt.Errorf("output path does not exist: %w", err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("output path %q is a file, not a directory", outputPath)
	}

	tempFile, err := os.CreateTemp(outputPath, filePrefix+"-*")
	if err != nil {
		return "", fmt.Errorf("failed creating temporary file in output directory: %w", err)
	}
	defer func() {
		_ = tempFile.Close()
	}()
	return tempFile.Name(), nil
}
