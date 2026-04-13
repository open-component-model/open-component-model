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
// +ocm:jsonschema-gen=true
type FileCleanupSpec struct {
	// Files is a list of file access specifications to clean up.
	// Each entry references a file that was buffered during a Get transformation.
	Files []accessv1alpha1.File `json:"files"`
}

// DeepCopyInto copies FileCleanupSpec into out.
func (in *FileCleanupSpec) DeepCopyInto(out *FileCleanupSpec) {
	*out = *in
	if in.Files != nil {
		out.Files = make([]accessv1alpha1.File, len(in.Files))
		copy(out.Files, in.Files)
	}
}

// FileCleanupOutput is the output of a FileCleanup transformation.
// +ocm:jsonschema-gen=true
type FileCleanupOutput struct {
	// CleanedFiles is the number of files that were successfully removed.
	CleanedFiles int `json:"cleanedFiles"`
}

// FileCleanupTransformation is a transformation specification that removes
// temporary files that were buffered to the local filesystem during Get
// transformations. It runs as a final node in the transformation graph,
// after all other transformations have completed.
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +ocm:jsonschema-gen=true
type FileCleanupTransformation struct {
	// +ocm:jsonschema-gen:enum=FileCleanup/v1alpha1
	Type   runtime.Type       `json:"type"`
	ID     string             `json:"id"`
	Spec   *FileCleanupSpec   `json:"spec"`
	Output *FileCleanupOutput `json:"output,omitempty"`
}

func (t *FileCleanupTransformation) GetType() runtime.Type    { return t.Type }
func (t *FileCleanupTransformation) SetType(typ runtime.Type) { t.Type = typ }

// DeepCopyTyped implements runtime.Typed.
func (in *FileCleanupTransformation) DeepCopyTyped() runtime.Typed {
	if in == nil {
		return nil
	}
	out := &FileCleanupTransformation{}
	out.Type = in.Type
	out.ID = in.ID
	if in.Spec != nil {
		out.Spec = &FileCleanupSpec{}
		in.Spec.DeepCopyInto(out.Spec)
	}
	if in.Output != nil {
		copied := *in.Output
		out.Output = &copied
	}
	return out
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

// fileBufferKind identifies which kind of resource produced a file buffer.
// Each kind maps to a fixed set of spec field expressions in the cleanup node.
type fileBufferKind int

const (
	fileBufferLocalBlob fileBufferKind = iota
	fileBufferOCIArtifact
	fileBufferHelm
)

// fileBufferHint records which transformation IDs are responsible for consuming
// a file buffer. The graph emits hints — it only knows IDs and resource kinds.
// The path-to-spec-field mapping lives here, not in graph construction.
type fileBufferHint struct {
	kind      fileBufferKind
	addID     string // Add transformation ID (consumer of the file buffer)
	convertID string // Convert transformation ID (Helm only)
}

// expressions returns the CEL spec-field expressions for this hint.
// Each expression references a *consumer's* spec field (not a producer's output),
// which causes the dependency discovery system to add edges from those
// consumers to the cleanup node — guaranteeing cleanup runs last.
func (h fileBufferHint) expressions() []string {
	switch h.kind {
	case fileBufferHelm:
		return []string{
			fmt.Sprintf("${%s.spec.chartFile}", h.convertID),
			// provFile is optional; cleanup transformer skips empty URIs.
			fmt.Sprintf("${%s.spec.?provFile}", h.convertID),
			fmt.Sprintf("${%s.spec.file}", h.addID),
		}
	default: // localBlob and OCIArtifact both use spec.file on the Add node
		return []string{
			fmt.Sprintf("${%s.spec.file}", h.addID),
		}
	}
}

// addFileCleanupTransformation appends a FileCleanup transformation to the graph.
// It is the only place that maps resource kinds to spec field path expressions.
func addFileCleanupTransformation(tgd *transformv1alpha1.TransformationGraphDefinition, hints []fileBufferHint) {
	if len(hints) == 0 {
		return
	}

	var files []any
	for _, hint := range hints {
		for _, expr := range hint.expressions() {
			files = append(files, expr)
		}
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
