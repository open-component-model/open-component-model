package blob_test

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	helmblob "ocm.software/open-component-model/bindings/go/helm/blob"
)

const (
	testdataChartWithProv = "../testdata/provenance/mychart-0.1.0.tgz"
	testdataProvFile      = "../testdata/provenance/mychart-0.1.0.tgz.prov"
	testdataChartOnly     = "../testdata/mychart-0.1.0.tgz"
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

// createTarBlobFromTestdata builds a tar archive from real testdata files.
func createTarBlobFromTestdata(t *testing.T, files map[string]string) *inmemory.Blob {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for tarName, filePath := range files {
		data, err := os.ReadFile(filePath)
		require.NoError(t, err, "should read testdata file %s", filePath)
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name: tarName,
			Size: int64(len(data)),
			Mode: 0o644,
		}))
		_, err = tw.Write(data)
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

func TestChartBlob_ExtractChartAndProv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		tarFiles     map[string]string
		chartSrcPath string
		provSrcPath  string
	}{
		{
			name: "flat paths with provenance",
			tarFiles: map[string]string{
				"mychart-0.1.0.tgz":      testdataChartWithProv,
				"mychart-0.1.0.tgz.prov": testdataProvFile,
			},
			chartSrcPath: testdataChartWithProv,
			provSrcPath:  testdataProvFile,
		},
		{
			name: "subdirectory paths with provenance",
			tarFiles: map[string]string{
				"helmRemoteChart123/mychart-0.1.0.tgz":      testdataChartWithProv,
				"helmRemoteChart123/mychart-0.1.0.tgz.prov": testdataProvFile,
			},
			chartSrcPath: testdataChartWithProv,
			provSrcPath:  testdataProvFile,
		},
		{
			name: "subdirectory tar.gz paths with provenance",
			tarFiles: map[string]string{
				"helmRemoteChart123/mychart-0.1.0.tar.gz":   testdataChartWithProv,
				"helmRemoteChart123/mychart-0.1.0.tgz.prov": testdataProvFile,
			},
			chartSrcPath: testdataChartWithProv,
			provSrcPath:  testdataProvFile,
		},
		{
			name: "chart without provenance",
			tarFiles: map[string]string{
				"mychart-0.1.0.tgz": testdataChartOnly,
			},
			chartSrcPath: testdataChartOnly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tarBlob := createTarBlobFromTestdata(t, tt.tarFiles)
			cb := helmblob.NewChartBlob(tarBlob)

			expectedChart, err := os.ReadFile(tt.chartSrcPath)
			require.NoError(t, err)

			chart, err := cb.ChartArchive()
			require.NoError(t, err)
			assert.Equal(t, expectedChart, readBlob(t, chart))

			prov, err := cb.ProvFile()
			require.NoError(t, err)
			if tt.provSrcPath != "" {
				require.NotNil(t, prov)
				expectedProv, err := os.ReadFile(tt.provSrcPath)
				require.NoError(t, err)
				assert.Equal(t, expectedProv, readBlob(t, prov))
			} else {
				assert.Nil(t, prov)
			}
		})
	}
}

func TestChartBlob_NoChartInTar(t *testing.T) {
	t.Parallel()

	tarBlob := createTarBlob(t, map[string][]byte{
		"readme.md": []byte("not a chart"),
	})

	cb := helmblob.NewChartBlob(tarBlob)

	_, err := cb.ChartArchive()
	require.Error(t, err)
	assert.ErrorIs(t, err, helmblob.ErrNoChartFound)
}

func TestChartBlob_EmptyTar(t *testing.T) {
	t.Parallel()

	tarBlob := createTarBlob(t, map[string][]byte{})

	cb := helmblob.NewChartBlob(tarBlob)

	_, err := cb.ChartArchive()
	require.Error(t, err)
	assert.ErrorIs(t, err, helmblob.ErrNoChartFound)
}

func TestChartBlob_ExtractionIsLazy(t *testing.T) {
	t.Parallel()

	tarBlob := createTarBlobFromTestdata(t, map[string]string{
		"mychart.tgz": testdataChartOnly,
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

	tarBlob := createTarBlobFromTestdata(t, map[string]string{
		"mychart.tgz": testdataChartOnly,
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
