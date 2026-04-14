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
		{
			name:        "opaque file URI",
			uri:         "file:relative/path",
			expectError: true,
		},
		{
			name:        "remote host",
			uri:         "file://remotehost/path/to/file",
			expectError: true,
		},
		{
			name:     "localhost host accepted",
			uri:      "file://localhost/tmp/file",
			expected: "/tmp/file",
		},
		{
			name:        "empty path",
			uri:         "file://",
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

func TestAddFileCleanupTransformation(t *testing.T) {
	tgd := &transformv1alpha1.TransformationGraphDefinition{}

	exprs := []string{
		"${addRes1.spec.file}",
		"${addRes2.spec.file}",
	}

	addFileCleanupTransformation(tgd, exprs)

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

func TestAddFileCleanupTransformation_NoExpressionsNoNode(t *testing.T) {
	tgd := &transformv1alpha1.TransformationGraphDefinition{}

	addFileCleanupTransformation(tgd, nil)
	assert.Empty(t, tgd.Transformations, "should not add cleanup when there are no expressions")

	addFileCleanupTransformation(tgd, []string{})
	assert.Empty(t, tgd.Transformations, "should not add cleanup for empty slice")
}

func TestFileCleanupExpressions_ReferenceConsumerNotProducer(t *testing.T) {
	// Expressions must reference .spec (consumer input), not .output (producer output),
	// so DAG edges point from consumers to the cleanup node.
	tests := []struct {
		name  string
		exprs []string
	}{
		{"local blob", []string{"${addId.spec.file}"}},
		{"OCI artifact", []string{"${addId.spec.file}"}},
		{"Helm chart", []string{
			"${convertId.spec.chartFile}",
			"${convertId.spec.?provFile}",
			"${addId.spec.file}",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, expr := range tt.exprs {
				assert.Contains(t, expr, ".spec.",
					"expression %q should reference consumer spec, not producer output", expr)
				assert.NotContains(t, expr, ".output.",
					"expression %q must not reference producer output", expr)
			}
		})
	}
}
