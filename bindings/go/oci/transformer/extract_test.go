package transformer

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/configuration/extract/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestTransformer_TransformBlob(t *testing.T) {
	tests := []struct {
		name        string
		setupBlob   func(t *testing.T) blob.ReadOnlyBlob
		expectError bool
	}{
		{
			name: "valid OCI artifact",
			setupBlob: func(t *testing.T) blob.ReadOnlyBlob {
				b, err := loadOCILayoutBlob("oci-layout.tar.gz")
				require.NoError(t, err)
				return b
			},
			expectError: false,
		},
		{
			name: "invalid blob data",
			setupBlob: func(t *testing.T) blob.ReadOnlyBlob {
				return inmemory.New(bytes.NewReader([]byte("not a valid tar")))
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transformer := New()
			inputBlob := tt.setupBlob(t)

			result, err := transformer.TransformBlob(context.Background(), inputBlob, nil)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)

				if mediaTypeAware, ok := result.(blob.MediaTypeAware); ok {
					mediaType, known := mediaTypeAware.MediaType()
					assert.True(t, known)
					assert.Equal(t, "application/tar", mediaType)
				}
			}
		})
	}
}

func TestTransformer_getDefaultFilename(t *testing.T) {
	transformer := New()

	tests := []struct {
		name     string
		digest   string
		expected string
	}{
		{"sha256 digest", "sha256:abc123def456", "abc123def456"},
		{"sha512 digest", "sha512:xyz789", "xyz789"},
		{"malformed digest", "invaliddigest", "invaliddigest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := transformer.getDefaultFilename(tt.digest)
			assert.Equal(t, tt.expected, result)
		})
	}
}


func TestTransformerIntegration(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	ociLayoutBlob, err := loadOCILayoutBlob("oci-layout.tar.gz")
	r.NoError(err)
	r.NotNil(ociLayoutBlob, "OCI layout blob should not be nil")

	transformer := New()
	// no config should default to all layers
	result, err := transformer.TransformBlob(ctx, ociLayoutBlob, nil)

	r.NoError(err, "Transformation should succeed")
	r.NotNil(result, "Result should not be nil")

	if mediaTypeAware, ok := result.(blob.MediaTypeAware); ok {
		mediaType, known := mediaTypeAware.MediaType()
		r.True(known, "Media type should be known")
		r.Equal("application/tar", mediaType, "Result should be tar format")
	}

	reader, err := result.ReadCloser()
	r.NoError(err, "Should be able to read result")

	expectedFiles := []string{"b4d308c16f0492bff4efa51ac9aab718bc819413008aec39007ef4abc309504a", "c25ba77a4c310dc0a36a4e587a216db0001488353a262b9147662ca6c2d25c69"}
	validateTarContents(t, reader, expectedFiles)

	t.Logf("Successfully transformed and validated OCI artifact")
}

// loadOCILayoutBlob loads an OCI layout tar file as a blob.
func loadOCILayoutBlob(layoutPath string) (blob.ReadOnlyBlob, error) {
	layoutData, err := os.ReadFile(filepath.Join("testdata", layoutPath))
	if err != nil {
		return nil, fmt.Errorf("failed to read OCI layout file: %w", err)
	}

	return &testBlob{data: layoutData}, nil
}

// testBlob is a simple implementation of blob.ReadOnlyBlob for testing.
type testBlob struct {
	data []byte
}

func (b *testBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(b.data)), nil
}

func (b *testBlob) Size() int64 {
	return int64(len(b.data))
}

// validateTarContents validates that specific files are present in the tar.
func validateTarContents(t *testing.T, reader io.ReadCloser, expectedFiles []string) {
	defer reader.Close()

	data, err := io.ReadAll(reader)
	require.NoError(t, err, "Should be able to read all data from tar")

	tarReader := tar.NewReader(bytes.NewReader(data))
	foundFiles := make(map[string]bool)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err, "Should be able to read tar header")

		filename := header.Name
		if strings.Contains(filename, "/") {
			parts := strings.Split(filename, "/")
			filename = parts[len(parts)-1]
		}

		if filename != "" {
			foundFiles[filename] = true
			t.Logf("Found file in tar: %s (original path: %s)", filename, header.Name)
		}
	}

	for _, expectedFile := range expectedFiles {
		require.True(t, foundFiles[expectedFile], "Expected file %s should be present in tar", expectedFile)
	}

	t.Logf("Successfully validated tar contains all expected files: %v", expectedFiles)
}

func TestTransformerWithRules(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	// Configure rules to extract specific layers with custom filenames
	config := &spec.Config{
		Type: runtime.NewVersionedType(spec.ConfigType, spec.Version),
		Rules: []spec.Rule{
			{
				Filename: "chart.tar.gz",
				LayerSelectors: []*spec.LayerSelector{
					{
						MatchProperties: map[string]string{
							spec.LayerMediaTypeKey: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
						},
					},
				},
			},
			{
				Filename: "chart.prov",
				LayerSelectors: []*spec.LayerSelector{
					{
						MatchProperties: map[string]string{
							spec.LayerMediaTypeKey: "application/vnd.cncf.helm.chart.provenance.v1.prov",
						},
					},
				},
			},
		},
	}

	ociLayoutBlob, err := loadOCILayoutBlob("oci-layout.tar.gz")
	r.NoError(err)
	r.NotNil(ociLayoutBlob)

	transformer := New()
	result, err := transformer.TransformBlob(ctx, ociLayoutBlob, config)

	r.NoError(err)
	r.NotNil(result)

	reader, err := result.ReadCloser()
	r.NoError(err)

	expectedFiles := []string{"chart.tar.gz", "chart.prov"}
	validateTarContents(t, reader, expectedFiles)

	t.Logf("Successfully filtered layers using Rules")
}

func TestTransformerWithIndexSelector(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	// select only the first layer (index 0)
	config := &spec.Config{
		Type: runtime.NewVersionedType(spec.ConfigType, spec.Version),
		Rules: []spec.Rule{
			{
				Filename: "first-layer.bin",
				LayerSelectors: []*spec.LayerSelector{
					{
						MatchProperties: map[string]string{
							spec.LayerIndexKey: "0",
						},
					},
				},
			},
		},
	}

	ociLayoutBlob, err := loadOCILayoutBlob("oci-layout.tar.gz")
	r.NoError(err)
	r.NotNil(ociLayoutBlob)

	transformer := New()
	result, err := transformer.TransformBlob(ctx, ociLayoutBlob, config)

	r.NoError(err)
	r.NotNil(result)

	reader, err := result.ReadCloser()
	r.NoError(err)

	data, err := io.ReadAll(reader)
	r.NoError(err)

	tarReader := tar.NewReader(bytes.NewReader(data))
	headerCount := 0
	for {
		_, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		r.NoError(err)
		headerCount++
	}

	r.Equal(1, headerCount, "should have exactly one layer when selecting index 0")
	t.Logf("Successfully selected layer by index")
}

func TestTransformerWithMatchExpressions(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	// test match expressions
	config := &spec.Config{
		Type: runtime.NewVersionedType(spec.ConfigType, spec.Version),
		Rules: []spec.Rule{
			{
				Filename: "helm-artifacts.tar",
				LayerSelectors: []*spec.LayerSelector{
					{
						MatchExpressions: []spec.LayerSelectorRequirement{
							{
								Key:      spec.LayerMediaTypeKey,
								Operator: spec.LayerSelectorOpIn,
								Values:   []string{"application/vnd.cncf.helm.chart.content.v1.tar+gzip", "application/vnd.cncf.helm.chart.provenance.v1.prov"},
							},
						},
					},
				},
			},
		},
	}

	ociLayoutBlob, err := loadOCILayoutBlob("oci-layout.tar.gz")
	r.NoError(err)
	r.NotNil(ociLayoutBlob)

	transformer := New()
	result, err := transformer.TransformBlob(ctx, ociLayoutBlob, config)

	r.NoError(err)
	r.NotNil(result)

	reader, err := result.ReadCloser()
	r.NoError(err)

	expectedFiles := []string{"helm-artifacts.tar"}
	validateTarContents(t, reader, expectedFiles)

	t.Logf("Successfully filtered layers using match expressions")
}

func TestTransformerWithRuleWithoutFilename(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	// Configure rule without filename - should fall back to default naming
	config := &spec.Config{
		Type: runtime.NewVersionedType(spec.ConfigType, spec.Version),
		Rules: []spec.Rule{
			{
				// Filename is empty - should use default naming
				LayerSelectors: []*spec.LayerSelector{
					{
						MatchProperties: map[string]string{
							spec.LayerIndexKey: "0",
						},
					},
				},
			},
		},
	}

	ociLayoutBlob, err := loadOCILayoutBlob("oci-layout.tar.gz")
	r.NoError(err)
	r.NotNil(ociLayoutBlob)

	transformer := New()
	result, err := transformer.TransformBlob(ctx, ociLayoutBlob, config)

	r.NoError(err)
	r.NotNil(result)

	reader, err := result.ReadCloser()
	r.NoError(err)

	// Should use default filename based on digest
	expectedFiles := []string{"b4d308c16f0492bff4efa51ac9aab718bc819413008aec39007ef4abc309504a"}
	validateTarContents(t, reader, expectedFiles)

	t.Logf("Successfully used default filename when rule doesn't specify one")
}

func TestTransformerWithHelmRulesWithoutFilenames(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)

	// Configure rules for Helm artifacts without filenames - should fall back to default naming
	config := &spec.Config{
		Type: runtime.NewVersionedType(spec.ConfigType, spec.Version),
		Rules: []spec.Rule{
			{
				// No filename specified for Helm chart
				LayerSelectors: []*spec.LayerSelector{
					{
						MatchProperties: map[string]string{
							spec.LayerMediaTypeKey: "application/vnd.cncf.helm.chart.content.v1.tar+gzip",
						},
					},
				},
			},
			{
				// No filename specified for Helm provenance
				LayerSelectors: []*spec.LayerSelector{
					{
						MatchProperties: map[string]string{
							spec.LayerMediaTypeKey: "application/vnd.cncf.helm.chart.provenance.v1.prov",
						},
					},
				},
			},
		},
	}

	ociLayoutBlob, err := loadOCILayoutBlob("oci-layout.tar.gz")
	r.NoError(err)
	r.NotNil(ociLayoutBlob)

	transformer := New()
	result, err := transformer.TransformBlob(ctx, ociLayoutBlob, config)

	r.NoError(err)
	r.NotNil(result)

	reader, err := result.ReadCloser()
	r.NoError(err)

	// Should use default filenames based on digest
	expectedFiles := []string{"b4d308c16f0492bff4efa51ac9aab718bc819413008aec39007ef4abc309504a", "c25ba77a4c310dc0a36a4e587a216db0001488353a262b9147662ca6c2d25c69"}
	validateTarContents(t, reader, expectedFiles)

	t.Logf("Successfully used default filenames for Helm artifacts when rules don't specify them")
}
