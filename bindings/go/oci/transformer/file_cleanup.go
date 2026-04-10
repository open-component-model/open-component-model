package transformer

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// FileCleanup is a transformer that removes temporary files from the local
// filesystem that were buffered during Get transformations.
// Cleanup is best-effort: missing files are silently skipped, and removal
// failures are logged but do not fail the transformation.
type FileCleanup struct {
	Scheme *runtime.Scheme
}

func (t *FileCleanup) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.FileCleanup
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to file cleanup: %w", err)
	}

	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for file cleanup transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.FileCleanupOutput{}
	}

	cleaned := 0
	for _, file := range transformation.Spec.Files {
		if file.URI == "" {
			continue
		}

		filePath, err := filePathFromURI(file.URI)
		if err != nil {
			slog.WarnContext(ctx, "skipping file cleanup: invalid URI",
				"uri", file.URI, "error", err)
			continue
		}

		if err := os.Remove(filePath); err != nil {
			if os.IsNotExist(err) {
				slog.DebugContext(ctx, "file already removed, skipping",
					"path", filePath)
				continue
			}
			slog.WarnContext(ctx, "failed to remove temporary file",
				"path", filePath, "error", err)
			continue
		}

		slog.DebugContext(ctx, "cleaned up temporary file", "path", filePath)
		cleaned++
	}

	transformation.Output.CleanedFiles = cleaned

	slog.InfoContext(ctx, "file buffer cleanup completed",
		"cleanedFiles", cleaned, "totalFiles", len(transformation.Spec.Files))

	return &transformation, nil
}

// filePathFromURI extracts the filesystem path from a file:// URI.
func filePathFromURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid URI %q: %w", uri, err)
	}
	if parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported URI scheme %q, expected \"file\"", parsed.Scheme)
	}
	return parsed.Path, nil
}
