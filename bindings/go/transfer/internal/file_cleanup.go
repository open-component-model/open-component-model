package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"

	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// FileCleanup is a transformer that removes temporary files from the local
// filesystem that were buffered during Get transformations.
// Cleanup is best-effort: missing files are silently skipped, and removal
// failures are logged but do not fail the transformation.
type FileCleanup struct {
	Scheme *runtime.Scheme
}

func (t *FileCleanup) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation ociv1alpha1.FileCleanup
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to file cleanup: %w", err)
	}

	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for file cleanup transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &ociv1alpha1.FileCleanupOutput{}
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

// fileBufferRef describes a CEL expression that resolves to a file access spec
// buffered to the local filesystem during a transformation. The expression
// references the file through its *consumer's* spec (e.g. the Add
// transformation's spec.file), not through the producer's output. This ensures
// the dependency discovery system creates edges from the consumer to the
// cleanup node, so cleanup only runs after consumers have finished reading
// the file.
type fileBufferRef struct {
	expression string
}

// addFileCleanupTransformation appends a FileCleanup transformation to the
// graph that removes all buffered temporary files. File references use CEL
// expressions that point to consumer spec fields (e.g. ${addId.spec.file}),
// which implicitly creates DAG edges from those consumers to the cleanup
// node via the dependency discovery system.
func addFileCleanupTransformation(tgd *transformv1alpha1.TransformationGraphDefinition, refs []fileBufferRef) {
	if len(refs) == 0 {
		return
	}

	files := make([]any, 0, len(refs))
	for _, ref := range refs {
		files = append(files, ref.expression)
	}

	cleanup := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ociv1alpha1.FileCleanupV1alpha1,
			ID:   "fileBufferCleanup",
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"files": files,
		}},
	}

	tgd.Transformations = append(tgd.Transformations, cleanup)
}

// collectLocalBlobFileRefs returns file buffer references for a local blob
// resource. The file is referenced through the Add transformation's spec,
// creating an Add → Cleanup dependency edge.
func collectLocalBlobFileRefs(addResourceID string) []fileBufferRef {
	return []fileBufferRef{
		{expression: fmt.Sprintf("${%s.spec.file}", addResourceID)},
	}
}

// collectOCIArtifactFileRefs returns file buffer references for an OCI
// artifact resource. The file is referenced through the Add transformation's
// spec, creating an Add → Cleanup dependency edge.
func collectOCIArtifactFileRefs(addResourceID string) []fileBufferRef {
	return []fileBufferRef{
		{expression: fmt.Sprintf("${%s.spec.file}", addResourceID)},
	}
}

// collectHelmFileRefs returns file buffer references for Helm chart
// transformations. Three temporary files may be created:
//   - The chart file from GetHelmChart, referenced through Convert's spec
//   - The optional prov file from GetHelmChart, referenced through Convert's spec
//   - The OCI layout file from ConvertHelmToOCI, referenced through Add's spec
//
// This creates Convert → Cleanup and Add → Cleanup dependency edges,
// ensuring cleanup runs after both Convert and Add have consumed their inputs.
func collectHelmFileRefs(convertResourceID, addResourceID string) []fileBufferRef {
	return []fileBufferRef{
		{expression: fmt.Sprintf("${%s.spec.chartFile}", convertResourceID)},
		// provFile is optional; the cleanup transformer skips empty URIs.
		{expression: fmt.Sprintf("${%s.spec.?provFile}", convertResourceID)},
		{expression: fmt.Sprintf("${%s.spec.file}", addResourceID)},
	}
}
