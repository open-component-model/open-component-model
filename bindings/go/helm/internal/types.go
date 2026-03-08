package internal

import (
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
)

// ChartData contains Helm chart contents as tgz archive, some metadata and optionally a provenance file.
type ChartData struct {
	Name      string
	Version   string
	ChartBlob *filesystem.Blob
	ProvBlob  *filesystem.Blob
}
