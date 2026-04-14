package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	accessv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

const (
	FileCleanupType    = "FileCleanup"
	fileCleanupVersion = "v1alpha1"
)

// FileCleanupVersionedType is the versioned type identifier for FileCleanup transformations.
var FileCleanupVersionedType = runtime.NewVersionedType(FileCleanupType, fileCleanupVersion)

// FileCleanupSpec is the input specification for a FileCleanup transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type FileCleanupSpec struct {
	// Files is a list of file access specifications to clean up.
	// Each entry references a file that was buffered during a Get transformation.
	Files []accessv1alpha1.File `json:"files"`
}

// FileCleanupOutput is the output of a FileCleanup transformation.
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type FileCleanupOutput struct {
	// CleanedFiles is the number of files that were successfully removed.
	CleanedFiles int `json:"cleanedFiles"`
}

// FileCleanupTransformation is a transformation specification that removes
// temporary files that were buffered to the local filesystem during Get
// transformations. It runs as a final node in the transformation graph,
// after all other transformations have completed.
// +k8s:deepcopy-gen=true
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type FileCleanupTransformation struct {
	// +ocm:jsonschema-gen:enum=FileCleanup/v1alpha1
	Type   runtime.Type       `json:"type"`
	ID     string             `json:"id"`
	Spec   *FileCleanupSpec   `json:"spec"`
	Output *FileCleanupOutput `json:"output,omitempty"`
}

// FileCleanup is a transformer that removes temporary files from the local
// filesystem that were buffered during Get transformations.
// Cleanup is best-effort: missing files are silently skipped, and removal
// failures are logged but do not fail the transformation.
type FileCleanup struct {
	Scheme *runtime.Scheme
}

func (t *FileCleanup) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation FileCleanupTransformation
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to file cleanup: %w", err)
	}

	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for file cleanup transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &FileCleanupOutput{}
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
// Only local file URIs are accepted: no opaque form, no remote host.
func filePathFromURI(uri string) (string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("invalid URI %q: %w", uri, err)
	}
	if parsed.Scheme != "file" {
		return "", fmt.Errorf("unsupported URI scheme %q, expected \"file\"", parsed.Scheme)
	}
	if parsed.Opaque != "" {
		return "", fmt.Errorf("opaque file URI %q not supported, use file:///path form", uri)
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		return "", fmt.Errorf("remote file URI %q not supported, host must be empty or localhost", uri)
	}
	if parsed.Path == "" {
		return "", fmt.Errorf("file URI %q has no path", uri)
	}
	return parsed.Path, nil
}

// addFileCleanupTransformation appends a FileCleanup transformation to the graph.
// fileExpressions is a list of CEL spec-field expressions referencing each consumer's
// spec field (not the producer's output), so the dependency discovery system creates
// edges from those consumers to the cleanup node — guaranteeing cleanup runs last.
func addFileCleanupTransformation(tgd *transformv1alpha1.TransformationGraphDefinition, fileExpressions []string) {
	if len(fileExpressions) == 0 {
		return
	}

	files := make([]any, len(fileExpressions))
	for i, expr := range fileExpressions {
		files[i] = expr
	}

	cleanup := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: FileCleanupVersionedType,
			ID:   "fileBufferCleanup",
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"files": files,
		}},
	}

	tgd.Transformations = append(tgd.Transformations, cleanup)
}
