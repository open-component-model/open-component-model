package oci_test

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"helm.sh/helm/v4/pkg/provenance"
	"helm.sh/helm/v4/pkg/registry"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/helm"
	"ocm.software/open-component-model/bindings/go/helm/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

func TestCopyChartToOCILayout_Success(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("..", "testdata")

	tests := []struct {
		name      string
		path      string
		provGPG   string
		provKeyID string
	}{
		{
			name: "non-packaged helm chart",
			path: filepath.Join(testDataDir, "mychart"),
		},
		{
			name: "packaged helm chart",
			path: filepath.Join(testDataDir, "mychart-0.1.0.tgz"),
		},
		{
			name:      "packaged helm chart with provenance file",
			path:      filepath.Join(testDataDir, "provenance", "mychart-0.1.0.tgz"),
			provGPG:   filepath.Join(testDataDir, "provenance", "pub.gpg"),
			provKeyID: "testkey",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chart := newReadOnlyChart(t, tt.path)

			result := oci.CopyChartToOCILayout(ctx, chart)
			require.NotNil(t, result)

			store, err := tar.ReadOCILayout(ctx, result)
			require.NoError(t, err)
			require.NotNil(t, store)
			t.Cleanup(func() {
				require.NoError(t, store.Close())
			})
			require.Len(t, store.Index.Manifests, 1)

			manifestRaw, err := store.Fetch(ctx, store.Index.Manifests[0])
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, manifestRaw.Close())
			})
			manifest := ociImageSpecV1.Manifest{}
			require.NoError(t, json.NewDecoder(manifestRaw).Decode(&manifest))

			assert.Equal(t, registry.ConfigMediaType, manifest.Config.MediaType, "expected config media type")
			require.GreaterOrEqual(t, len(manifest.Layers), 1, "expected at least one layer")
			assert.Equal(t, registry.ChartLayerMediaType, manifest.Layers[0].MediaType, "expected first layer to be chart layer")

			if tt.provGPG != "" {
				signatory, err := provenance.NewFromKeyring(tt.provGPG, tt.provKeyID)
				require.NoError(t, err, "failed to create signatory from GPG keyring")

				t.Run("provenance verification", func(t *testing.T) {
					require.Len(t, manifest.Layers, 2, "expected two layers for chart and provenance file")
					assert.Equal(t, registry.ProvLayerMediaType, manifest.Layers[1].MediaType, "expected second layer to be provenance file")

					chartLayer, err := store.Fetch(ctx, manifest.Layers[0])
					require.NoError(t, err)
					t.Cleanup(func() {
						require.NoError(t, chartLayer.Close())
					})
					chartData, err := io.ReadAll(chartLayer)
					require.NoError(t, err, "failed to read chart layer")

					provLayer, err := store.Fetch(ctx, manifest.Layers[1])
					require.NoError(t, err)
					t.Cleanup(func() {
						require.NoError(t, provLayer.Close())
					})
					provData, err := io.ReadAll(provLayer)
					require.NoError(t, err, "failed to read provenance layer")

					_, err = signatory.Verify(chartData, provData, filepath.Base(tt.path))
					require.NoError(t, err, "failed to verify provenance file")
				})
			}
		})
	}
}

func TestCopyChartToOCILayout_TagMatchesVersion(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("..", "testdata")

	chart := newReadOnlyChart(t, filepath.Join(testDataDir, "mychart-0.1.0.tgz"))

	result := oci.CopyChartToOCILayout(ctx, chart)
	require.NotNil(t, result)

	store, err := tar.ReadOCILayout(ctx, result)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	require.Len(t, store.Index.Manifests, 1)
	desc := store.Index.Manifests[0]
	assert.Equal(t, chart.Version, desc.Annotations[ociImageSpecV1.AnnotationRefName],
		"OCI layout tag should match the chart version")
}

func TestCopyChartToOCILayout_ConfigContent(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("..", "testdata")

	chart := newReadOnlyChart(t, filepath.Join(testDataDir, "mychart-0.1.0.tgz"))

	result := oci.CopyChartToOCILayout(ctx, chart)
	require.NotNil(t, result)

	store, err := tar.ReadOCILayout(ctx, result)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})
	require.Len(t, store.Index.Manifests, 1)

	manifestRaw, err := store.Fetch(ctx, store.Index.Manifests[0])
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, manifestRaw.Close())
	})
	manifest := ociImageSpecV1.Manifest{}
	require.NoError(t, json.NewDecoder(manifestRaw).Decode(&manifest))

	configRaw, err := store.Fetch(ctx, manifest.Config)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, configRaw.Close())
	})

	var config map[string]string
	require.NoError(t, json.NewDecoder(configRaw).Decode(&config))
	assert.Equal(t, chart.Name, config["name"], "config name should match chart name")
	assert.Equal(t, chart.Version, config["version"], "config version should match chart version")
}

func TestCopyChartToOCILayout_ExactlyOneLayerWithoutProvenance(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("..", "testdata")

	chart := newReadOnlyChart(t, filepath.Join(testDataDir, "mychart-0.1.0.tgz"))
	// Explicitly ensure no provenance blob
	chart.ProvBlob = nil

	result := oci.CopyChartToOCILayout(ctx, chart)
	require.NotNil(t, result)

	store, err := tar.ReadOCILayout(ctx, result)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})
	require.Len(t, store.Index.Manifests, 1)

	manifestRaw, err := store.Fetch(ctx, store.Index.Manifests[0])
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, manifestRaw.Close())
	})
	manifest := ociImageSpecV1.Manifest{}
	require.NoError(t, json.NewDecoder(manifestRaw).Decode(&manifest))

	require.Len(t, manifest.Layers, 1, "chart without provenance should have exactly one layer")
	assert.Equal(t, registry.ChartLayerMediaType, manifest.Layers[0].MediaType)
}

func TestCopyChartToOCILayout_ChartLayerDataIntegrity(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("..", "testdata")
	chartPath := filepath.Join(testDataDir, "mychart-0.1.0.tgz")

	chart := newReadOnlyChart(t, chartPath)

	// Read original chart bytes for comparison
	originalData, err := os.ReadFile(chartPath)
	require.NoError(t, err)

	result := oci.CopyChartToOCILayout(ctx, chart)
	require.NotNil(t, result)

	store, err := tar.ReadOCILayout(ctx, result)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	manifestRaw, err := store.Fetch(ctx, store.Index.Manifests[0])
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, manifestRaw.Close())
	})
	manifest := ociImageSpecV1.Manifest{}
	require.NoError(t, json.NewDecoder(manifestRaw).Decode(&manifest))

	chartLayer, err := store.Fetch(ctx, manifest.Layers[0])
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, chartLayer.Close())
	})

	layerData, err := io.ReadAll(chartLayer)
	require.NoError(t, err)
	assert.Equal(t, originalData, layerData, "chart layer data should match original tgz file")
}

func TestCopyChartToOCILayout_BlobMediaType(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("..", "testdata")

	chart := newReadOnlyChart(t, filepath.Join(testDataDir, "mychart-0.1.0.tgz"))

	result := oci.CopyChartToOCILayout(ctx, chart)
	require.NotNil(t, result)

	mediaType, known := result.MediaType()
	assert.True(t, known, "media type should be known")
	assert.Equal(t, layout.MediaTypeOCIImageLayoutTarGzipV1, mediaType)

	// Consume the blob so the goroutine finishes
	_, err := tar.ReadOCILayout(ctx, result)
	require.NoError(t, err)
}

func TestCopyChartToOCILayout_DescriptorAvailableAfterBlobConsumed(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("..", "testdata")

	chart := newReadOnlyChart(t, filepath.Join(testDataDir, "mychart-0.1.0.tgz"))

	result := oci.CopyChartToOCILayout(ctx, chart)
	require.NotNil(t, result)

	// Consume the blob first
	store, err := tar.ReadOCILayout(ctx, result)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})

	// Now Descriptor should be available without blocking
	desc, err := result.Descriptor(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, desc.Digest, "descriptor digest should not be empty")
	assert.Greater(t, desc.Size, int64(0), "descriptor size should be positive")
}

func TestCopyChartToOCILayout_EmptyChartDir(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("..", "testdata")

	chart := newReadOnlyChart(t, filepath.Join(testDataDir, "mychart-0.1.0.tgz"))
	// Empty ChartDir means cleanup (os.RemoveAll) should be a no-op
	chart.ChartDir = ""

	result := oci.CopyChartToOCILayout(ctx, chart)
	require.NotNil(t, result)

	store, err := tar.ReadOCILayout(ctx, result)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, store.Close())
	})
	require.Len(t, store.Index.Manifests, 1)
}

func TestCopyChartToOCILayout_NilChartBlobReturnsError(t *testing.T) {
	ctx := t.Context()

	chart := &helm.ChartData{
		Name:      "test",
		Version:   "1.0.0",
		ChartBlob: nil,
	}

	result := oci.CopyChartToOCILayout(ctx, chart)
	require.NotNil(t, result)

	// Consume the blob to let the goroutine finish
	rc, err := result.ReadCloser()
	require.NoError(t, err)
	_, _ = io.ReadAll(rc)
	_ = rc.Close()

	// The error should surface via Descriptor
	_, descErr := result.Descriptor(ctx)
	require.Error(t, descErr, "nil ChartBlob should cause an error")
	assert.Contains(t, descErr.Error(), "chart blob must not be nil")
}

// newReadOnlyChart creates a helm.ChartData from a path to testdata.
// It handles both directory charts (by packaging them) and pre-packaged tgz charts.
func newReadOnlyChart(t *testing.T, path string) *helm.ChartData {
	t.Helper()

	ch, err := loader.Load(path)
	require.NoError(t, err, "failed to load helm chart from %q", path)

	fi, err := os.Stat(path)
	require.NoError(t, err)

	result := &helm.ChartData{
		Name:    ch.Name(),
		Version: ch.Metadata.Version,
	}

	if fi.IsDir() {
		tmpDir := t.TempDir()
		path, err = chartutil.Save(ch, tmpDir)
		require.NoError(t, err, "failed to save chart to temp dir")
		result.ChartDir = tmpDir
	}

	result.ChartBlob, err = filesystem.GetBlobFromOSPath(path)
	require.NoError(t, err)

	provPath := path + ".prov"
	if _, statErr := os.Stat(provPath); statErr == nil {
		result.ProvBlob, err = filesystem.GetBlobFromOSPath(provPath)
		require.NoError(t, err)
	}

	return result
}
