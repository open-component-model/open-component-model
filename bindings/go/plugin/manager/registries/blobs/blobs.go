package blobs

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

// CreateBlobData creates a blob based on the location.
func CreateBlobData(location types.Location) (b blob.Blob, err error) {
	switch location.LocationType {
	case types.LocationTypeLocalFile:
		b, err = filesystem.GetBlobFromOSPath(location.Value)
	default:
		return nil, fmt.Errorf("unsupported location type: %s", location.LocationType)
	}

	if err != nil {
		return nil, err
	}

	if location.MediaType != "" {
		if mtOverrideable, ok := b.(blob.MediaTypeOverrideable); ok {
			mtOverrideable.SetMediaType(location.MediaType)
		}
	}

	return b, nil
}
