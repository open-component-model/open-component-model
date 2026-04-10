package internal

import (
	"fmt"

	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1/meta"
)

// fileBufferRef describes a CEL expression that resolves to a file access spec
// produced by a Get or Convert transformation. These are collected during graph
// construction and fed to the FileCleanup transformation.
type fileBufferRef struct {
	// expression is the CEL expression that resolves to the file, e.g.
	// "${getResourceID.output.file}" or "${convertResourceID.output.file}".
	expression string
}

// addFileCleanupTransformation appends a FileCleanup transformation to the graph
// that references all buffered file outputs. The dependency discovery system
// will automatically resolve the CEL expressions and ensure the cleanup node
// runs after all referenced transformations complete.
//
// uploadIDs are the IDs of the AddComponentVersion (upload) transformations.
// The cleanup spec includes a dependsOn field with CEL references to upload outputs,
// ensuring the cleanup node only runs after all uploads have finished consuming
// the buffered files.
func addFileCleanupTransformation(tgd *transformv1alpha1.TransformationGraphDefinition, refs []fileBufferRef, uploadIDs []string) {
	if len(refs) == 0 {
		return
	}

	files := make([]any, 0, len(refs))
	for _, ref := range refs {
		files = append(files, ref.expression)
	}

	// Build CEL references to upload outputs so the dependency discovery
	// system creates edges from uploads → cleanup. Without this, the cleanup
	// would only depend on Get transformations and could race with uploads
	// that are still consuming the buffered files.
	dependsOn := make([]any, 0, len(uploadIDs))
	for _, uploadID := range uploadIDs {
		dependsOn = append(dependsOn, fmt.Sprintf("${%s.output}", uploadID))
	}

	cleanup := transformv1alpha1.GenericTransformation{
		TransformationMeta: meta.TransformationMeta{
			Type: ociv1alpha1.FileCleanupV1alpha1,
			ID:   "fileBufferCleanup",
		},
		Spec: &runtime.Unstructured{Data: map[string]any{
			"files":     files,
			"dependsOn": dependsOn,
		}},
	}

	tgd.Transformations = append(tgd.Transformations, cleanup)
}

// collectLocalBlobFileRefs returns the file buffer references produced by a
// GetLocalResource transformation.
func collectLocalBlobFileRefs(getResourceID string) []fileBufferRef {
	return []fileBufferRef{
		{expression: fmt.Sprintf("${%s.output.file}", getResourceID)},
	}
}

// collectOCIArtifactFileRefs returns the file buffer references produced by a
// GetOCIArtifact transformation.
func collectOCIArtifactFileRefs(getResourceID string) []fileBufferRef {
	return []fileBufferRef{
		{expression: fmt.Sprintf("${%s.output.file}", getResourceID)},
	}
}

// collectHelmFileRefs returns the file buffer references produced by Helm
// transformations. This includes the chart file (and optional prov file) from
// GetHelmChart, and the OCI layout file from ConvertHelmToOCI.
func collectHelmFileRefs(getResourceID, convertResourceID string) []fileBufferRef {
	return []fileBufferRef{
		{expression: fmt.Sprintf("${%s.output.chartFile}", getResourceID)},
		// provFile is optional; the cleanup transformer skips empty URIs.
		{expression: fmt.Sprintf("${%s.output.?provFile}", getResourceID)},
		{expression: fmt.Sprintf("${%s.output.file}", convertResourceID)},
	}
}
