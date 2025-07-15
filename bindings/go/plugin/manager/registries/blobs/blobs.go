package blobs

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// CreateBlobData creates a blob based on the location.
func CreateBlobData(location types.Location) (blob.Blob, error) {
	switch location.LocationType {
	case types.LocationTypeLocalFile:
		return filesystem.GetBlobFromOSPath(location.Value)
	default:
		return nil, fmt.Errorf("unsupported location type: %s", location.LocationType)
	}
}
