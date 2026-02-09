package transformer

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob"
)

// determineOutputPath determines the output path for buffering the blob content.
// If the outputPath is empty, it creates a temporary file with an appropriate extension based on the media type of the blob content.
// If the outputPath is provided, it ensures that the directory exists.
func determineOutputPath(outputPath string, filePrefix string, blobContent blob.ReadOnlyBlob) (string, error) {
	if outputPath == "" {
		fileExt := ""
		if mediaTypeAware, ok := blobContent.(blob.MediaTypeAware); ok {
			if mediaType, ok := mediaTypeAware.MediaType(); ok {
				fileExt = mediaTypExtMap[mediaType]
			}
		}

		if fileExt == "" {
			slog.Warn("unable to determine file extension from media type, setting .bin extension")
			fileExt = ".bin"
		} else {
			fileExt = fmt.Sprintf(".%s", fileExt)
		}

		// Create a temporary file
		tempFile, err := os.CreateTemp("", fmt.Sprintf("%s-*%s", filePrefix, fileExt))
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
