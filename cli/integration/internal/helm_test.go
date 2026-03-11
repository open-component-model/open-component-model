package internal

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"helm.sh/helm/v4/pkg/registry"
)

// createTestOCILayout builds a minimal OCI layout directory with the given config media type and layers.
// Each layer is specified as a mediaType + content pair.
func createTestOCILayout(t *testing.T, dir string, configMediaType string, layers []struct {
	mediaType string
	content   []byte
}) {
	t.Helper()

	// Write oci-layout marker
	require := func(err error, msg string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", msg, err)
		}
	}

	require(os.MkdirAll(filepath.Join(dir, "blobs", "sha256"), 0o755), "create blobs dir")

	// Build layer descriptors and write blobs
	var layerDescs []ociImageSpecV1.Descriptor
	for _, l := range layers {
		dgst := fmt.Sprintf("%x", sha256.Sum256(l.content))
		require(os.WriteFile(filepath.Join(dir, "blobs", "sha256", dgst), l.content, 0o644), "write layer blob")
		layerDescs = append(layerDescs, ociImageSpecV1.Descriptor{
			MediaType: l.mediaType,
			Digest:    godigest.NewDigestFromEncoded(godigest.SHA256, dgst),
			Size:      int64(len(l.content)),
		})
	}

	// Config blob
	configContent := []byte("{}")
	configDigest := fmt.Sprintf("%x", sha256.Sum256(configContent))
	require(os.WriteFile(filepath.Join(dir, "blobs", "sha256", configDigest), configContent, 0o644), "write config blob")

	manifest := ociImageSpecV1.Manifest{
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config: ociImageSpecV1.Descriptor{
			MediaType: configMediaType,
			Digest:    godigest.NewDigestFromEncoded(godigest.SHA256, configDigest),
			Size:      int64(len(configContent)),
		},
		Layers: layerDescs,
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := fmt.Sprintf("%x", sha256.Sum256(manifestData))
	require(os.WriteFile(filepath.Join(dir, "blobs", "sha256", manifestDigest), manifestData, 0o644), "write manifest blob")

	index := ociImageSpecV1.Index{
		MediaType: ociImageSpecV1.MediaTypeImageIndex,
		Manifests: []ociImageSpecV1.Descriptor{
			{
				MediaType: ociImageSpecV1.MediaTypeImageManifest,
				Digest:    godigest.NewDigestFromEncoded(godigest.SHA256, manifestDigest),
				Size:      int64(len(manifestData)),
			},
		},
	}

	indexData, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	require(os.WriteFile(filepath.Join(dir, "index.json"), indexData, 0o644), "write index.json")
}

func TestParseHelmOCILayout(t *testing.T) {
	dir := t.TempDir()
	chartContent := []byte("chart-data")
	createTestOCILayout(t, dir, registry.ConfigMediaType, []struct {
		mediaType string
		content   []byte
	}{
		{mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip", content: chartContent},
	})

	layout := ParseHelmOCILayout(t, dir)

	assert.NotEmpty(t, layout.Index.Manifests, "index should have manifests")
	assert.Equal(t, registry.ConfigMediaType, layout.Manifest.Config.MediaType)
	assert.Len(t, layout.Manifest.Layers, 1)
	assert.Equal(t, dir, layout.Dir)
}

func TestFindLayerByMediaType(t *testing.T) {
	dir := t.TempDir()
	createTestOCILayout(t, dir, registry.ConfigMediaType, []struct {
		mediaType string
		content   []byte
	}{
		{mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip", content: []byte("chart")},
		{mediaType: "application/vnd.cncf.helm.chart.provenance.v1.prov", content: []byte("prov")},
	})

	layout := ParseHelmOCILayout(t, dir)

	t.Run("finds existing layer", func(t *testing.T) {
		layer := layout.FindLayerByMediaType("helm.chart.content")
		assert.NotNil(t, layer)
		assert.Contains(t, layer.MediaType, "helm.chart.content")
	})

	t.Run("finds provenance layer", func(t *testing.T) {
		layer := layout.FindLayerByMediaType("helm.chart.provenance")
		assert.NotNil(t, layer)
		assert.Contains(t, layer.MediaType, "helm.chart.provenance")
	})

	t.Run("returns nil for missing layer", func(t *testing.T) {
		layer := layout.FindLayerByMediaType("nonexistent")
		assert.Nil(t, layer)
	})
}

func TestReadLayerBlob(t *testing.T) {
	dir := t.TempDir()
	expected := []byte("chart-blob-content")
	createTestOCILayout(t, dir, registry.ConfigMediaType, []struct {
		mediaType string
		content   []byte
	}{
		{mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip", content: expected},
	})

	layout := ParseHelmOCILayout(t, dir)
	layer := layout.FindLayerByMediaType("helm.chart.content")

	actual := layout.ReadLayerBlob(t, layer)
	assert.Equal(t, expected, actual)
}

func TestAssertHelmChartLayer(t *testing.T) {
	dir := t.TempDir()
	createTestOCILayout(t, dir, registry.ConfigMediaType, []struct {
		mediaType string
		content   []byte
	}{
		{mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip", content: []byte("chart-data")},
	})

	layout := ParseHelmOCILayout(t, dir)
	// Should not panic or fail
	layout.AssertHelmChartLayer(t)
}

func TestAssertChartContentEquals(t *testing.T) {
	dir := t.TempDir()
	chartContent := []byte("original-chart-content")
	createTestOCILayout(t, dir, registry.ConfigMediaType, []struct {
		mediaType string
		content   []byte
	}{
		{mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip", content: chartContent},
	})

	originalPath := filepath.Join(t.TempDir(), "chart.tgz")
	if err := os.WriteFile(originalPath, chartContent, 0o644); err != nil {
		t.Fatal(err)
	}

	layout := ParseHelmOCILayout(t, dir)
	layout.AssertChartContentEquals(t, originalPath)
}

func TestAssertProvContentEquals(t *testing.T) {
	dir := t.TempDir()
	provContent := []byte("provenance-data")
	createTestOCILayout(t, dir, registry.ConfigMediaType, []struct {
		mediaType string
		content   []byte
	}{
		{mediaType: "application/vnd.cncf.helm.chart.content.v1.tar+gzip", content: []byte("chart")},
		{mediaType: "application/vnd.cncf.helm.chart.provenance.v1.prov", content: provContent},
	})

	originalPath := filepath.Join(t.TempDir(), "chart.prov")
	if err := os.WriteFile(originalPath, provContent, 0o644); err != nil {
		t.Fatal(err)
	}

	layout := ParseHelmOCILayout(t, dir)
	layout.AssertProvContentEquals(t, originalPath)
}
