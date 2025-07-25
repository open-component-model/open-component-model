package transformer

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
)

func TestOCIArtifactTransformer_TransformBlob(t *testing.T) {
	tests := []struct {
		name        string
		setupBlob   func(t *testing.T) *inmemory.Blob
		expectError bool
	}{
		{
			name: "valid helm chart OCI artifact",
			setupBlob: func(t *testing.T) *inmemory.Blob {
				return createHelmChartOCIBlob(t)
			},
			expectError: false,
		},
		{
			name: "invalid blob data",
			setupBlob: func(t *testing.T) *inmemory.Blob {
				return inmemory.New(bytes.NewReader([]byte("not a valid tar")))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := NewOCIArtifactTransformer()
			inputBlob := tt.setupBlob(t)

			result, err := transformer.TransformBlob(context.TODO(), inputBlob, nil)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)

				reader, err := result.ReadCloser()
				require.NoError(t, err)
				defer reader.Close()

				tarReader := tar.NewReader(reader)
				files := make(map[string]bool)

				for {
					header, err := tarReader.Next()
					if err == io.EOF {
						break
					}
					require.NoError(t, err)
					files[header.Name] = true
				}

				assert.Greater(t, len(files), 0)
			}
		})
	}
}

func TestOCIArtifactTransformer_getFilenameForMediaType(t *testing.T) {
	transformer := NewOCIArtifactTransformer()

	tests := []struct {
		mediaType string
		expected  string
	}{
		{"application/tar", "layer.tar"},
		{"application/tar+gzip", "layer.tar.gz"},
		{"application/json", "layer.json"},
		{"application/octet-stream", "layer.bin"},
		{MediaTypeHelmChart, "layer.tar.gz"},
		{MediaTypeHelmProvenance, "layer.bin"},
		{MediaTypeHelmConfig, "layer.json"},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			result := transformer.getFilenameForMediaType(tt.mediaType)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func createHelmChartOCIBlob(t *testing.T) *inmemory.Blob {
	t.Helper()

	var ociLayoutBuffer bytes.Buffer
	tarWriter := tar.NewWriter(&ociLayoutBuffer)

	chartContent := createMockChartContent(t)
	provenanceContent := []byte("mock provenance data")
	configContent := []byte(`{"name": "test-chart", "version": "1.0.0"}`)

	chartDigest := digest.FromBytes(chartContent)
	provDigest := digest.FromBytes(provenanceContent)
	configDigest := digest.FromBytes(configContent)

	manifest := ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		Config: ociImageSpecV1.Descriptor{
			MediaType: MediaTypeHelmConfig,
			Digest:    configDigest,
			Size:      int64(len(configContent)),
		},
		Layers: []ociImageSpecV1.Descriptor{
			{
				MediaType: MediaTypeHelmChart,
				Digest:    chartDigest,
				Size:      int64(len(chartContent)),
			},
			{
				MediaType: MediaTypeHelmProvenance,
				Digest:    provDigest,
				Size:      int64(len(provenanceContent)),
			},
		},
	}

	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)

	manifestDigest := digest.FromBytes(manifestBytes)

	index := ociImageSpecV1.Index{
		Versioned: specs.Versioned{
			SchemaVersion: 2,
		},
		Manifests: []ociImageSpecV1.Descriptor{
			{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    manifestDigest,
				Size:      int64(len(manifestBytes)),
			},
		},
	}

	indexBytes, err := json.Marshal(index)
	require.NoError(t, err)

	ociLayoutContent := []byte(`{"imageLayoutVersion": "1.0.0"}`)
	files := map[string][]byte{
		"oci-layout":                           ociLayoutContent,
		"index.json":                           indexBytes,
		"blobs/sha256/" + chartDigest.Hex():    chartContent,
		"blobs/sha256/" + provDigest.Hex():     provenanceContent,
		"blobs/sha256/" + configDigest.Hex():   configContent,
		"blobs/sha256/" + manifestDigest.Hex(): manifestBytes,
	}

	for filename, content := range files {
		header := &tar.Header{
			Name: filename,
			Size: int64(len(content)),
			Mode: 0644,
		}

		require.NoError(t, tarWriter.WriteHeader(header))
		_, err := tarWriter.Write(content)
		require.NoError(t, err)
	}

	require.NoError(t, tarWriter.Close())

	return inmemory.New(bytes.NewReader(ociLayoutBuffer.Bytes()))
}

func createMockChartContent(t *testing.T) []byte {
	t.Helper()

	var chartBuffer bytes.Buffer
	gzWriter := gzip.NewWriter(&chartBuffer)
	tarWriter := tar.NewWriter(gzWriter)

	files := map[string]string{
		"Chart.yaml": `apiVersion: v2
name: test-chart
version: 1.0.0
description: A test chart
`,
		"values.yaml": `# Default values
replicaCount: 1
`,
		"templates/deployment.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
spec:
  replicas: {{ .Values.replicaCount }}
`,
	}

	for filename, content := range files {
		header := &tar.Header{
			Name: filename,
			Size: int64(len(content)),
			Mode: 0644,
		}

		require.NoError(t, tarWriter.WriteHeader(header))
		_, err := tarWriter.Write([]byte(content))
		require.NoError(t, err)
	}

	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzWriter.Close())

	return chartBuffer.Bytes()
}
