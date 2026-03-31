package blob_test

import (
	"archive/tar"
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	helmblob "ocm.software/open-component-model/bindings/go/helm/blob"
)

func createTarBlob(t *testing.T, files map[string][]byte) *inmemory.Blob {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: name,
			Size: int64(len(content)),
			Mode: 0o644,
		}))
		_, err := tw.Write(content)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return inmemory.New(&buf)
}

func readBlob(t *testing.T, b interface{ ReadCloser() (io.ReadCloser, error) }) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

func TestChartBlob_ChartArchiveAndProvFile(t *testing.T) {
	t.Parallel()

	chartData := []byte("fake-chart-content")
	provData := []byte("fake-prov-content")

	tarBlob := createTarBlob(t, map[string][]byte{
		"mychart-0.1.0.tgz":      chartData,
		"mychart-0.1.0.tgz.prov": provData,
	})

	cb := helmblob.NewChartBlob(tarBlob)

	chart, err := cb.ChartArchive()
	require.NoError(t, err)
	assert.Equal(t, chartData, readBlob(t, chart))

	prov, err := cb.ProvFile()
	require.NoError(t, err)
	require.NotNil(t, prov)
	assert.Equal(t, provData, readBlob(t, prov))
}

func TestChartBlob_ChartArchiveWithoutProv(t *testing.T) {
	t.Parallel()

	chartData := []byte("fake-chart-content")
	tarBlob := createTarBlob(t, map[string][]byte{
		"mychart-0.1.0.tgz": chartData,
	})

	cb := helmblob.NewChartBlob(tarBlob)

	chart, err := cb.ChartArchive()
	require.NoError(t, err)
	assert.Equal(t, chartData, readBlob(t, chart))

	prov, err := cb.ProvFile()
	require.NoError(t, err)
	assert.Nil(t, prov)
}

func TestChartBlob_NoChartInTar(t *testing.T) {
	t.Parallel()

	tarBlob := createTarBlob(t, map[string][]byte{
		"readme.md": []byte("not a chart"),
	})

	cb := helmblob.NewChartBlob(tarBlob)

	_, err := cb.ChartArchive()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no chart (.tgz) found")
}

func TestChartBlob_EmptyTar(t *testing.T) {
	t.Parallel()

	tarBlob := createTarBlob(t, map[string][]byte{})

	cb := helmblob.NewChartBlob(tarBlob)

	_, err := cb.ChartArchive()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no chart (.tgz) found")
}

func TestChartBlob_SubdirectoryPaths(t *testing.T) {
	t.Parallel()

	chartData := []byte("chart-in-subdir")
	provData := []byte("prov-in-subdir")

	tarBlob := createTarBlob(t, map[string][]byte{
		"helmRemoteChart123/mychart-0.1.0.tgz":      chartData,
		"helmRemoteChart123/mychart-0.1.0.tgz.prov": provData,
	})

	cb := helmblob.NewChartBlob(tarBlob)

	chart, err := cb.ChartArchive()
	require.NoError(t, err)
	assert.Equal(t, chartData, readBlob(t, chart))

	prov, err := cb.ProvFile()
	require.NoError(t, err)
	require.NotNil(t, prov)
	assert.Equal(t, provData, readBlob(t, prov))
}

func TestChartBlob_ExtractionIsLazy(t *testing.T) {
	t.Parallel()

	chartData := []byte("lazy-chart")
	tarBlob := createTarBlob(t, map[string][]byte{
		"mychart.tgz": chartData,
	})

	cb := helmblob.NewChartBlob(tarBlob)

	// Multiple calls should return the same result (sync.Once)
	chart1, err := cb.ChartArchive()
	require.NoError(t, err)

	chart2, err := cb.ChartArchive()
	require.NoError(t, err)

	assert.Equal(t, readBlob(t, chart1), readBlob(t, chart2))
}

func TestChartBlob_ReadCloserReturnsTarContent(t *testing.T) {
	t.Parallel()

	chartData := []byte("chart-content")
	tarBlob := createTarBlob(t, map[string][]byte{
		"mychart.tgz": chartData,
	})

	cb := helmblob.NewChartBlob(tarBlob)

	// ReadCloser on the ChartBlob itself should return the raw tar
	rc, err := cb.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}
