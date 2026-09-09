package location

import (
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func Write(location types.Location, b blob.ReadOnlyBlob) error {
	switch location.LocationType {
	case types.LocationTypeLocalFile, types.LocationTypeUnixNamedPipe:
		return filesystem.CopyBlobToOSPath(b, location.Value)
	default:
		return fmt.Errorf("unsupported target location type %q", location.LocationType)
	}
}

func Read(location types.Location) (blob.ReadOnlyBlob, error) {
	switch location.LocationType {
	case types.LocationTypeLocalFile, types.LocationTypeUnixNamedPipe:
		return filesystem.GetBlobFromOSPath(location.Value)
	default:
		return nil, fmt.Errorf("unsupported resource location type %q", location.LocationType)
	}
}
