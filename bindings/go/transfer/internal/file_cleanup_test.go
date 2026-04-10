package internal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	accessv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// --- FileCleanup transformer tests ---

func newFileCleanupScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	s.MustRegisterWithAlias(&FileCleanupTransformation{}, FileCleanupVersionedType)
	return s
}

func TestFileCleanup_Transform(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name            string
		setup           func(t *testing.T) []accessv1alpha1.File
		expectedCleaned int
	}{
		{
			name: "cleans up multiple temp files",
			setup: func(t *testing.T) []accessv1alpha1.File {
				t.Helper()
				dir := t.TempDir()

				f1 := filepath.Join(dir, "resource-001")
				f2 := filepath.Join(dir, "oci-artifact-002")
				require.NoError(t, os.WriteFile(f1, []byte("blob1"), 0o600))
				require.NoError(t, os.WriteFile(f2, []byte("blob2"), 0o600))

				return []accessv1alpha1.File{
					{URI: "file://" + f1},
					{URI: "file://" + f2},
				}
			},
			expectedCleaned: 2,
		},
		{
			name: "skips non-existent files",
			setup: func(t *testing.T) []accessv1alpha1.File {
				t.Helper()
				return []accessv1alpha1.File{
					{URI: "file:///tmp/does-not-exist-cleanup-test-12345"},
				}
			},
			expectedCleaned: 0,
		},
		{
			name: "skips empty URIs",
			setup: func(t *testing.T) []accessv1alpha1.File {
				t.Helper()
				dir := t.TempDir()
				f := filepath.Join(dir, "real-file")
				require.NoError(t, os.WriteFile(f, []byte("data"), 0o600))

				return []accessv1alpha1.File{
					{URI: ""},
					{URI: "file://" + f},
					{URI: ""},
				}
			},
			expectedCleaned: 1,
		},
		{
			name: "handles mixed existing and non-existing files",
			setup: func(t *testing.T) []accessv1alpha1.File {
				t.Helper()
				dir := t.TempDir()
				existing := filepath.Join(dir, "exists")
				require.NoError(t, os.WriteFile(existing, []byte("data"), 0o600))

				return []accessv1alpha1.File{
					{URI: "file://" + existing},
					{URI: "file:///tmp/cleanup-nonexistent-xyz"},
				}
			},
			expectedCleaned: 1,
		},
		{
			name: "handles empty file list",
			setup: func(t *testing.T) []accessv1alpha1.File {
				t.Helper()
				return []accessv1alpha1.File{}
			},
			expectedCleaned: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := newFileCleanupScheme()
			transformer := &FileCleanup{Scheme: scheme}

			files := tt.setup(t)
			spec := &FileCleanupTransformation{
				Type: FileCleanupVersionedType,
				ID:   "testCleanup",
				Spec: &FileCleanupSpec{
					Files: files,
				},
			}

			result, err := transformer.Transform(ctx, spec)
			require.NoError(t, err)
			require.NotNil(t, result)

			transformed, ok := result.(*FileCleanupTransformation)
			require.True(t, ok)
			require.NotNil(t, transformed.Output)
			assert.Equal(t, tt.expectedCleaned, transformed.Output.CleanedFiles)
		})
	}
}

func TestFileCleanup_Transform_NilSpec(t *testing.T) {
	ctx := t.Context()
	scheme := newFileCleanupScheme()
	transformer := &FileCleanup{Scheme: scheme}

	spec := &FileCleanupTransformation{
		Type: FileCleanupVersionedType,
		ID:   "testCleanup",
		Spec: nil,
	}

	result, err := transformer.Transform(ctx, spec)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "spec is required")
}

func TestFileCleanup_Transform_VerifiesFilesRemoved(t *testing.T) {
	ctx := t.Context()
	scheme := newFileCleanupScheme()
	transformer := &FileCleanup{Scheme: scheme}

	dir := t.TempDir()
	f1 := filepath.Join(dir, "to-remove")
	require.NoError(t, os.WriteFile(f1, []byte("temporary data"), 0o600))

	// File exists before cleanup
	_, err := os.Stat(f1)
	require.NoError(t, err)

	spec := &FileCleanupTransformation{
		Type: FileCleanupVersionedType,
		ID:   "testCleanup",
		Spec: &FileCleanupSpec{
			Files: []accessv1alpha1.File{
				{URI: "file://" + f1},
			},
		},
	}

	result, err := transformer.Transform(ctx, spec)
	require.NoError(t, err)

	transformed := result.(*FileCleanupTransformation)
	assert.Equal(t, 1, transformed.Output.CleanedFiles)

	// File no longer exists after cleanup
	_, err = os.Stat(f1)
	assert.True(t, os.IsNotExist(err))
}

func TestFilePathFromURI(t *testing.T) {
	tests := []struct {
		name        string
		uri         string
		expected    string
		expectError bool
	}{
		{
			name:     "valid file URI",
			uri:      "file:///tmp/oci-artifact-abc123",
			expected: "/tmp/oci-artifact-abc123",
		},
		{
			name:     "file URI with nested path",
			uri:      "file:///var/tmp/buffers/resource-001",
			expected: "/var/tmp/buffers/resource-001",
		},
		{
			name:        "non-file scheme",
			uri:         "https://example.com/file",
			expectError: true,
		},
		{
			name:        "invalid URI",
			uri:         "://broken",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := filePathFromURI(tt.uri)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// --- Graph helper tests ---

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
	assert.Equal(t, FileCleanupVersionedType, cleanup.Type)

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
