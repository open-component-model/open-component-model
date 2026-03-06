package helm

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// ChartData contains Helm chart contents as tgz archive, some metadata and optionally a provenance file.
type ChartData struct {
	Name      string
	Version   string
	ChartBlob *filesystem.Blob
	ProvBlob  *filesystem.Blob

	// ChartDir is the directory where the chart is downloaded to. This is cleaned after the writer
	// has finished with copying it later in copyChartToOCILayoutAsync.
	ChartDir string
}
