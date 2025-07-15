package blobs

import (
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// CreateBlobData creates a blob based on the location.
func CreateBlobData(location types.Location) (blob.Blob, error) {
	switch location.LocationType {
	case types.LocationTypeLocalFile:
		file, err := os.Stat(location.Value)
		if err != nil {
			return nil, err
		}

		fileBlob, err := filesystem.GetBlobFromOSPath(file.Name())
		if err != nil {
			return nil, err
		}

		return fileBlob, nil
	default:
		return nil, fmt.Errorf("unsupported location type: %s", location.LocationType)
	}
}
