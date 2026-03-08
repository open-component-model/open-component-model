package oci_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"helm.sh/helm/v4/pkg/provenance"
	"helm.sh/helm/v4/pkg/registry"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/helm/internal"
	"ocm.software/open-component-model/bindings/go/helm/internal/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

func TestCopyChartToOCILayout_Success(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("../../testdata")

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

			result, err := oci.CopyChartToOCILayout(ctx, chart, t.TempDir())
			require.NoError(t, err)
			require.NotNil(t, result)

			store, err := tar.ReadOCILayout(ctx, result.Blob)
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
	testDataDir := filepath.Join("../../testdata")

	chart := newReadOnlyChart(t, filepath.Join(testDataDir, "mychart-0.1.0.tgz"))

	result, err := oci.CopyChartToOCILayout(ctx, chart, t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, result)

	store, err := tar.ReadOCILayout(ctx, result.Blob)
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
	testDataDir := filepath.Join("../../testdata")

	chart := newReadOnlyChart(t, filepath.Join(testDataDir, "mychart-0.1.0.tgz"))

	result, err := oci.CopyChartToOCILayout(ctx, chart, t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, result)

	store, err := tar.ReadOCILayout(ctx, result.Blob)
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

func TestCopyChartToOCILayout_Matrix(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("../../testdata")
	chartPath := filepath.Join(testDataDir, "mychart-0.1.0.tgz")

	tests := []struct {
		name  string
		chart func(t *testing.T) *internal.ChartData
		check func(t *testing.T, result *oci.Result, chart *internal.ChartData)
	}{
		{
			name: "exactly one layer without provenance",
			chart: func(t *testing.T) *internal.ChartData {
				t.Helper()
				c := newReadOnlyChart(t, chartPath)
				c.ProvBlob = nil
				return c
			},
			check: func(t *testing.T, result *oci.Result, _ *internal.ChartData) {
				t.Helper()
				_, manifest := readManifest(t, ctx, result)
				require.Len(t, manifest.Layers, 1, "chart without provenance should have exactly one layer")
				assert.Equal(t, registry.ChartLayerMediaType, manifest.Layers[0].MediaType)
			},
		},
		{
			name:  "chart layer data matches original tgz",
			chart: func(t *testing.T) *internal.ChartData { t.Helper(); return newReadOnlyChart(t, chartPath) },
			check: func(t *testing.T, result *oci.Result, _ *internal.ChartData) {
				t.Helper()
				originalData, err := os.ReadFile(chartPath)
				require.NoError(t, err)

				store, manifest := readManifest(t, ctx, result)
				chartLayer, err := store.Fetch(ctx, manifest.Layers[0])
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, chartLayer.Close()) })

				layerData, err := io.ReadAll(chartLayer)
				require.NoError(t, err)
				assert.Equal(t, originalData, layerData, "chart layer data should match original tgz file")
			},
		},
		{
			name:  "blob media type is OCI layout tar gzip",
			chart: func(t *testing.T) *internal.ChartData { t.Helper(); return newReadOnlyChart(t, chartPath) },
			check: func(t *testing.T, result *oci.Result, _ *internal.ChartData) {
				t.Helper()
				mediaType, known := result.Blob.MediaType()
				assert.True(t, known, "media type should be known")
				assert.Equal(t, layout.MediaTypeOCIImageLayoutTarGzipV1, mediaType)
			},
		},
		{
			name:  "descriptor is available immediately",
			chart: func(t *testing.T) *internal.ChartData { t.Helper(); return newReadOnlyChart(t, chartPath) },
			check: func(t *testing.T, result *oci.Result, _ *internal.ChartData) {
				t.Helper()
				assert.NotEmpty(t, result.Desc.Digest, "descriptor digest should not be empty")
				assert.Greater(t, result.Desc.Size, int64(0), "descriptor size should be positive")
			},
		},
		{
			name:  "OCI layout store has exactly one manifest",
			chart: func(t *testing.T) *internal.ChartData { t.Helper(); return newReadOnlyChart(t, chartPath) },
			check: func(t *testing.T, result *oci.Result, _ *internal.ChartData) {
				t.Helper()
				store, err := tar.ReadOCILayout(ctx, result.Blob)
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, store.Close()) })
				require.Len(t, store.Index.Manifests, 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chart := tt.chart(t)
			result, err := oci.CopyChartToOCILayout(ctx, chart, t.TempDir())
			require.NoError(t, err)
			require.NotNil(t, result)
			tt.check(t, result, chart)
		})
	}
}

func TestCopyChartToOCILayout_DigestsMatchContent(t *testing.T) {
	ctx := t.Context()
	testDataDir := filepath.Join("../../testdata")

	tests := []struct {
		name string
		path string
	}{
		{
			name: "packaged helm chart",
			path: filepath.Join(testDataDir, "mychart-0.1.0.tgz"),
		},
		{
			name: "packaged helm chart with provenance",
			path: filepath.Join(testDataDir, "provenance", "mychart-0.1.0.tgz"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chart := newReadOnlyChart(t, tt.path)

			result, err := oci.CopyChartToOCILayout(ctx, chart, t.TempDir())
			require.NoError(t, err)
			require.NotNil(t, result)

			store, err := tar.ReadOCILayout(ctx, result.Blob)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, store.Close()) })

			require.Len(t, store.Index.Manifests, 1)
			indexDesc := store.Index.Manifests[0]

			// Validate the manifest digest matches the index descriptor.
			manifestRaw, err := store.Fetch(ctx, indexDesc)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, manifestRaw.Close()) })
			manifestBytes, err := io.ReadAll(manifestRaw)
			require.NoError(t, err)

			assert.Equal(t, indexDesc.Digest, digest.FromBytes(manifestBytes),
				"index descriptor digest should match manifest content")
			assert.Equal(t, indexDesc.Size, int64(len(manifestBytes)),
				"index descriptor size should match manifest content length")

			var manifest ociImageSpecV1.Manifest
			require.NoError(t, json.Unmarshal(manifestBytes, &manifest))

			// Validate config layer digest.
			configRaw, err := store.Fetch(ctx, manifest.Config)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, configRaw.Close()) })
			configBytes, err := io.ReadAll(configRaw)
			require.NoError(t, err)

			assert.Equal(t, manifest.Config.Digest, digest.FromBytes(configBytes),
				"config descriptor digest should match config content")
			assert.Equal(t, manifest.Config.Size, int64(len(configBytes)),
				"config descriptor size should match config content length")

			// Validate all layer digests.
			for i, layerDesc := range manifest.Layers {
				layerReader, err := store.Fetch(ctx, layerDesc)
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, layerReader.Close()) })
				layerBytes, err := io.ReadAll(layerReader)
				require.NoError(t, err)

				assert.Equal(t, layerDesc.Digest, digest.FromBytes(layerBytes),
					"layer[%d] descriptor digest should match layer content", i)
				assert.Equal(t, layerDesc.Size, int64(len(layerBytes)),
					"layer[%d] descriptor size should match layer content length", i)
			}

			// Validate result descriptor matches the index descriptor.
			assert.Equal(t, indexDesc.Digest, result.Desc.Digest,
				"result descriptor digest should match index descriptor digest")
			assert.Equal(t, indexDesc.Size, result.Desc.Size,
				"result descriptor size should match index descriptor size")
		})
	}
}

func TestCopyChartToOCILayout_NilChartBlobReturnsError(t *testing.T) {
	ctx := t.Context()

	chart := &internal.ChartData{
		Name:      "test",
		Version:   "1.0.0",
		ChartBlob: nil,
	}

	_, err := oci.CopyChartToOCILayout(ctx, chart, t.TempDir())
	require.Error(t, err, "nil ChartBlob should cause an error")
	assert.Contains(t, err.Error(), "chart blob must not be nil")
}

// readManifest is a test helper that opens the OCI layout store from a Result
// and decodes the single manifest. It registers cleanup for the store and manifest reader.
func readManifest(t *testing.T, ctx context.Context, result *oci.Result) (*tar.CloseableReadOnlyStore, ociImageSpecV1.Manifest) {
	t.Helper()

	store, err := tar.ReadOCILayout(ctx, result.Blob)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.Len(t, store.Index.Manifests, 1)

	manifestRaw, err := store.Fetch(ctx, store.Index.Manifests[0])
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, manifestRaw.Close()) })

	var manifest ociImageSpecV1.Manifest
	require.NoError(t, json.NewDecoder(manifestRaw).Decode(&manifest))
	return store, manifest
}

// newReadOnlyChart creates a helm.ChartData from a path to testdata.
// It handles both directory charts (by packaging them) and pre-packaged tgz charts.
func newReadOnlyChart(t *testing.T, path string) *internal.ChartData {
	t.Helper()

	ch, err := loader.Load(path)
	require.NoError(t, err, "failed to load helm chart from %q", path)

	fi, err := os.Stat(path)
	require.NoError(t, err)

	result := &internal.ChartData{
		Name:    ch.Name(),
		Version: ch.Metadata.Version,
	}

	if fi.IsDir() {
		tmpDir := t.TempDir()
		path, err = chartutil.Save(ch, tmpDir)
		require.NoError(t, err, "failed to save chart to temp dir")
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
