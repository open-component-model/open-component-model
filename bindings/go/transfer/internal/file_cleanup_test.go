package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

func TestCollectLocalBlobFileRefs(t *testing.T) {
	refs := collectLocalBlobFileRefs("compAddMyResource")

	require.Len(t, refs, 1)
	assert.Equal(t, "${compAddMyResource.spec.file}", refs[0].expression)
}

func TestCollectOCIArtifactFileRefs(t *testing.T) {
	refs := collectOCIArtifactFileRefs("compAddMyImage")

	require.Len(t, refs, 1)
	assert.Equal(t, "${compAddMyImage.spec.file}", refs[0].expression)
}

func TestCollectHelmFileRefs(t *testing.T) {
	refs := collectHelmFileRefs("compConvertMyChart", "compAddMyChart")

	require.Len(t, refs, 3)
	assert.Equal(t, "${compConvertMyChart.spec.chartFile}", refs[0].expression,
		"chart file should reference Convert spec")
	assert.Equal(t, "${compConvertMyChart.spec.?provFile}", refs[1].expression,
		"prov file should reference Convert spec with optional accessor")
	assert.Equal(t, "${compAddMyChart.spec.file}", refs[2].expression,
		"OCI layout file should reference Add spec")
}

func TestCollectHelmFileRefs_DependsOnBothConvertAndAdd(t *testing.T) {
	refs := collectHelmFileRefs("convert1", "add1")

	// Extract unique transformation IDs referenced in expressions.
	// The CEL inspector would extract the root identifier before the first dot.
	ids := make(map[string]bool)
	for _, ref := range refs {
		// Parse "${id.spec.field}" to extract "id"
		expr := ref.expression
		if len(expr) > 2 && expr[0] == '$' && expr[1] == '{' {
			inner := expr[2 : len(expr)-1]
			for i, c := range inner {
				if c == '.' {
					ids[inner[:i]] = true
					break
				}
			}
		}
	}

	assert.True(t, ids["convert1"], "should depend on Convert transformation")
	assert.True(t, ids["add1"], "should depend on Add transformation")
}

func TestAddFileCleanupTransformation(t *testing.T) {
	tgd := &transformv1alpha1.TransformationGraphDefinition{}

	refs := []fileBufferRef{
		{expression: "${addRes1.spec.file}"},
		{expression: "${addRes2.spec.file}"},
	}

	addFileCleanupTransformation(tgd, refs)

	require.Len(t, tgd.Transformations, 1)

	cleanup := tgd.Transformations[0]
	assert.Equal(t, "fileBufferCleanup", cleanup.ID)
	assert.Equal(t, ociv1alpha1.FileCleanupV1alpha1, cleanup.Type)

	files, ok := cleanup.Spec.Data["files"].([]any)
	require.True(t, ok, "spec.files should be []any")
	require.Len(t, files, 2)
	assert.Equal(t, "${addRes1.spec.file}", files[0])
	assert.Equal(t, "${addRes2.spec.file}", files[1])
}

func TestAddFileCleanupTransformation_NoRefsNoNode(t *testing.T) {
	tgd := &transformv1alpha1.TransformationGraphDefinition{}

	addFileCleanupTransformation(tgd, nil)
	assert.Empty(t, tgd.Transformations, "should not add cleanup when there are no file refs")

	addFileCleanupTransformation(tgd, []fileBufferRef{})
	assert.Empty(t, tgd.Transformations, "should not add cleanup for empty slice")
}

func TestFileRefsReferenceConsumerNotProducer(t *testing.T) {
	// Verify that all collector functions produce expressions referencing
	// .spec (consumer input) rather than .output (producer output).
	// This ensures the DAG edges point from consumers to cleanup,
	// not from producers to cleanup.

	tests := []struct {
		name string
		refs []fileBufferRef
	}{
		{"local blob", collectLocalBlobFileRefs("addId")},
		{"OCI artifact", collectOCIArtifactFileRefs("addId")},
		{"Helm chart", collectHelmFileRefs("convertId", "addId")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, ref := range tt.refs {
				assert.Contains(t, ref.expression, ".spec.",
					"expression %q should reference consumer spec, not producer output", ref.expression)
				assert.NotContains(t, ref.expression, ".output.",
					"expression %q must not reference producer output", ref.expression)
			}
		})
	}
}
