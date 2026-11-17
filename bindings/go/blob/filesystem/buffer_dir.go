package filesystem

import (
	"fmt"
	"os"
)

// BufferDir returns the directory a caller buffers content in: outputPath when it is set,
// otherwise tempDir, which is created when it does not exist yet. An empty result stands
// for the default temporary directory of the operating system.
func BufferDir(outputPath, tempDir string) (string, error) {
	if outputPath != "" || tempDir == "" {
		return outputPath, nil
	}
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create temporary directory: %w", err)
	}
	return tempDir, nil
}
