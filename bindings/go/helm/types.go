package helm

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// ReadOnlyChart contains Helm chart contents as tgz archive, some metadata and optionally a provenance file.
type ReadOnlyChart struct {
	Name          string
	Version       string
	ChartBlob     *filesystem.Blob
	ChartBlobPath string
	ProvBlob      *filesystem.Blob
	ProvBlobPath  string

	// ChartTempDir is the temporary directory where the chart is downloaded to. This is cleaned after the writer
	// has finished with copying it later in copyChartToOCILayoutAsync.
	ChartTempDir string
}
