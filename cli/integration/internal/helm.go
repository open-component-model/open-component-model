package internal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"
)

// HelmOCILayout represents a parsed OCI layout containing a helm chart.
type HelmOCILayout struct {
	Index    ociImageSpecV1.Index
	Manifest ociImageSpecV1.Manifest
	Dir      string
}

// ParseHelmOCILayout reads an OCI layout directory and parses the first manifest.
// It verifies the layout has a valid index and manifest structure.
func ParseHelmOCILayout(t *testing.T, dir string) *HelmOCILayout {
	t.Helper()
	r := require.New(t)

	indexData, err := os.ReadFile(filepath.Join(dir, "index.json"))
	r.NoError(err, "should be able to read OCI index.json")

	var index ociImageSpecV1.Index
	r.NoError(json.Unmarshal(indexData, &index), "should be able to parse OCI index")
	r.NotEmpty(index.Manifests, "OCI index should contain at least one manifest")

	manifestDesc := index.Manifests[0]
	manifestPath := filepath.Join(dir, "blobs", manifestDesc.Digest.Algorithm().String(), manifestDesc.Digest.Encoded())
	manifestData, err := os.ReadFile(manifestPath)
	r.NoError(err, "should be able to read OCI manifest")

	var manifest ociImageSpecV1.Manifest
	r.NoError(json.Unmarshal(manifestData, &manifest), "should be able to parse OCI manifest")

	return &HelmOCILayout{
		Index:    index,
		Manifest: manifest,
		Dir:      dir,
	}
}

// FindLayerByMediaType returns the first layer descriptor whose media type contains the given substring.
func (l *HelmOCILayout) FindLayerByMediaType(substring string) *ociImageSpecV1.Descriptor {
	for i, layer := range l.Manifest.Layers {
		if strings.Contains(layer.MediaType, substring) {
			return &l.Manifest.Layers[i]
		}
	}
	return nil
}

// ReadLayerBlob reads the blob content of the given layer descriptor.
func (l *HelmOCILayout) ReadLayerBlob(t *testing.T, layer *ociImageSpecV1.Descriptor) []byte {
	t.Helper()
	r := require.New(t)

	blobPath := filepath.Join(l.Dir, "blobs", layer.Digest.Algorithm().String(), layer.Digest.Encoded())
	data, err := os.ReadFile(blobPath)
	r.NoError(err, "should be able to read layer blob")
	return data
}

// AssertHelmChartLayer verifies the manifest has a helm chart config and at least one chart content layer.
func (l *HelmOCILayout) AssertHelmChartLayer(t *testing.T) {
	t.Helper()

	assert.Equal(t, registry.ConfigMediaType, l.Manifest.Config.MediaType, "config should have helm config media type")
	require.NotEmpty(t, l.Manifest.Layers, "manifest should have at least one layer")

	chartLayer := l.FindLayerByMediaType("helm.chart.content")
	require.NotNil(t, chartLayer, "manifest should contain a helm chart content layer")

	data := l.ReadLayerBlob(t, chartLayer)
	assert.Greater(t, len(data), 0, "chart blob should not be empty")
}

// AssertChartContentEquals verifies the chart content layer matches the expected file.
func (l *HelmOCILayout) AssertChartContentEquals(t *testing.T, originalChartPath string) {
	t.Helper()
	r := require.New(t)

	chartLayer := l.FindLayerByMediaType("helm.chart.content")
	r.NotNil(chartLayer, "should find helm chart content layer in manifest")

	expected, err := os.ReadFile(originalChartPath)
	r.NoError(err, "should be able to read original helm chart")

	actual := l.ReadLayerBlob(t, chartLayer)
	r.Equal(expected, actual, "downloaded chart blob should match original helm chart")
}

// AssertProvContentEquals verifies the provenance layer matches the expected file.
func (l *HelmOCILayout) AssertProvContentEquals(t *testing.T, originalProvPath string) {
	t.Helper()
	r := require.New(t)

	provLayer := l.FindLayerByMediaType("helm.chart.provenance")
	r.NotNil(provLayer, "should find helm chart provenance layer in manifest")

	expected, err := os.ReadFile(originalProvPath)
	r.NoError(err, "should be able to read original prov file")

	actual := l.ReadLayerBlob(t, provLayer)
	r.Equal(expected, actual, "downloaded prov blob should match original prov file")
}
