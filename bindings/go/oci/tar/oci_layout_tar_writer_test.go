package tar

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOCILayoutTarWriter(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewOCILayoutWriter(buf)
	require.NotNil(t, writer)
}

func TestOCILayoutTarWriter_Push(t *testing.T) {
	tests := []struct {
		name        string
		desc        ociImageSpecV1.Descriptor
		data        []byte
		expectError bool
	}{
		{
			name: "valid manifest",
			desc: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    digest.FromString("test content"),
				Size:      12,
			},
			data:        []byte("test content"),
			expectError: false,
		},
		{
			name: "valid blob",
			desc: ociImageSpecV1.Descriptor{
				MediaType: "application/octet-stream",
				Digest:    digest.FromString("test blob"),
				Size:      9,
			},
			data:        []byte("test blob"),
			expectError: false,
		},
		{
			name: "invalid digest",
			desc: ociImageSpecV1.Descriptor{
				MediaType: "application/octet-stream",
				Digest:    "invalid:digest",
				Size:      9,
			},
			data:        []byte("test blob"),
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			writer := NewOCILayoutWriter(buf)
			defer writer.Close()

			err := writer.Push(context.Background(), tt.desc, bytes.NewReader(tt.data))
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Verify the content was written correctly
			tarReader := tar.NewReader(buf)
			header, err := tarReader.Next()
			require.NoError(t, err)
			require.Equal(t, "blobs/sha256/"+tt.desc.Digest.Encoded(), header.Name)
			require.Equal(t, tt.desc.Size, header.Size)

			content, err := io.ReadAll(tarReader)
			require.NoError(t, err)
			assert.Equal(t, tt.data, content)
		})
	}
}

func TestOCILayoutTarWriter_Tag(t *testing.T) {
	tests := []struct {
		name        string
		desc        ociImageSpecV1.Descriptor
		reference   string
		expectError bool
	}{
		{
			name: "valid tag",
			desc: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    digest.FromString("test content"),
				Size:      12,
			},
			reference:   "test-tag",
			expectError: false,
		},
		{
			name: "empty reference",
			desc: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    digest.FromString("test content"),
				Size:      12,
			},
			reference:   "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			writer := NewOCILayoutWriter(buf)
			defer writer.Close()

			// First push the content
			err := writer.Push(context.Background(), tt.desc, bytes.NewReader([]byte("test content")))
			require.NoError(t, err)

			// Then try to tag it
			err = writer.Tag(context.Background(), tt.desc, tt.reference)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Verify the tag was set correctly
			resolved, err := writer.Resolve(context.Background(), tt.reference)
			assert.NoError(t, err)
			assert.Equal(t, tt.desc.Digest, resolved.Digest)
		})
	}
}

func TestOCILayoutTarWriter_Close(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewOCILayoutWriter(buf)

	// Push some content
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromString("test content"),
		Size:      12,
	}
	err := writer.Push(context.Background(), desc, bytes.NewReader([]byte("test content")))
	require.NoError(t, err)

	// Close the writer
	err = writer.Close()
	require.NoError(t, err)

	// Verify the index.json and oci-layout files were written
	tarReader := tar.NewReader(buf)
	files := make(map[string][]byte)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		content, err := io.ReadAll(tarReader)
		require.NoError(t, err)
		files[header.Name] = content
	}

	// Verify index.json
	indexJSON, ok := files[ociImageSpecV1.ImageIndexFile]
	require.True(t, ok)
	var index ociImageSpecV1.Index
	err = json.Unmarshal(indexJSON, &index)
	require.NoError(t, err)
	assert.Equal(t, int(2), index.SchemaVersion)

	// Verify oci-layout
	layoutJSON, ok := files[ociImageSpecV1.ImageLayoutFile]
	require.True(t, ok)
	var layout ociImageSpecV1.ImageLayout
	err = json.Unmarshal(layoutJSON, &layout)
	require.NoError(t, err)
	assert.Equal(t, ociImageSpecV1.ImageLayoutVersion, layout.Version)
}

func TestOCILayoutTarWriter_Exists(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewOCILayoutWriter(buf)
	defer writer.Close()

	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromString("test content"),
		Size:      12,
	}

	// Check existence before pushing
	exists, err := writer.Exists(context.Background(), desc)
	require.NoError(t, err)
	assert.False(t, exists)

	// Push the content
	err = writer.Push(context.Background(), desc, bytes.NewReader([]byte("test content")))
	require.NoError(t, err)

	// Check existence after pushing
	exists, err = writer.Exists(context.Background(), desc)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestOCILayoutTarWriter_Fetch(t *testing.T) {
	buf := &bytes.Buffer{}
	writer := NewOCILayoutWriter(buf)
	defer writer.Close()

	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromString("test content"),
		Size:      12,
	}

	// Fetch should always return ErrUnsupported
	reader, err := writer.Fetch(context.Background(), desc)
	assert.Error(t, err)
	assert.Nil(t, reader)
}

func TestOCILayoutTarWriter_Resolve(t *testing.T) {
	tests := []struct {
		name        string
		desc        ociImageSpecV1.Descriptor
		reference   string
		expectError bool
	}{
		{
			name: "existing reference",
			desc: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    digest.FromString("test content"),
				Size:      12,
			},
			reference:   "test-tag",
			expectError: false,
		},
		{
			name: "non-existent reference",
			desc: ociImageSpecV1.Descriptor{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    digest.FromString("test content"),
				Size:      12,
			},
			reference:   "non-existent",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			writer := NewOCILayoutWriter(buf)
			defer writer.Close()

			// Push and tag the content if we expect it to exist
			if !tt.expectError {
				err := writer.Push(context.Background(), tt.desc, bytes.NewReader([]byte("test content")))
				require.NoError(t, err)
				err = writer.Tag(context.Background(), tt.desc, tt.reference)
				require.NoError(t, err)
			}

			// Try to resolve the reference
			resolved, err := writer.Resolve(context.Background(), tt.reference)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.desc.Digest, resolved.Digest)
		})
	}
}

func TestBlobPath(t *testing.T) {
	tests := []struct {
		name        string
		digest      digest.Digest
		expectError bool
	}{
		{
			name:        "valid digest",
			digest:      digest.FromString("test content"),
			expectError: false,
		},
		{
			name:        "invalid digest",
			digest:      "invalid:digest",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := blobPath(tt.digest)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			expectedPath := "blobs/sha256/" + tt.digest.Encoded()
			assert.Equal(t, expectedPath, path)
		})
	}
}

func TestMemoryResolver(t *testing.T) {
	resolver := newMemoryResolver()
	desc := ociImageSpecV1.Descriptor{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Digest:    digest.FromString("test content"),
		Size:      12,
	}

	// Test Tag
	err := resolver.Tag(context.Background(), desc, "test-tag")
	require.NoError(t, err)

	// Test Resolve
	resolved, err := resolver.Resolve(context.Background(), "test-tag")
	require.NoError(t, err)
	assert.Equal(t, desc.Digest, resolved.Digest)

	// Test Map
	refMap := resolver.Map()
	require.Len(t, refMap, 1)
	assert.Equal(t, desc.Digest, refMap["test-tag"].Digest)

	// Test TagSet
	tagSet := resolver.TagSet(desc)
	assert.True(t, tagSet.Contains("test-tag"))

	// Test Untag
	resolver.Untag("test-tag")
	resolved, err = resolver.Resolve(context.Background(), "test-tag")
	assert.Error(t, err)
}

func TestDeleteAnnotationRefName(t *testing.T) {
	tests := []struct {
		name     string
		desc     ociImageSpecV1.Descriptor
		expected ociImageSpecV1.Descriptor
	}{
		{
			name: "with ref name annotation",
			desc: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					ociImageSpecV1.AnnotationRefName: "test-tag",
					"other":                          "value",
				},
			},
			expected: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					"other": "value",
				},
			},
		},
		{
			name: "without ref name annotation",
			desc: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					"other": "value",
				},
			},
			expected: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					"other": "value",
				},
			},
		},
		{
			name: "only ref name annotation",
			desc: ociImageSpecV1.Descriptor{
				Annotations: map[string]string{
					ociImageSpecV1.AnnotationRefName: "test-tag",
				},
			},
			expected: ociImageSpecV1.Descriptor{
				Annotations: nil,
			},
		},
		{
			name: "nil annotations",
			desc: ociImageSpecV1.Descriptor{
				Annotations: nil,
			},
			expected: ociImageSpecV1.Descriptor{
				Annotations: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deleteAnnotationRefName(tt.desc)
			assert.Equal(t, tt.expected.Annotations, result.Annotations)
		})
	}
}
