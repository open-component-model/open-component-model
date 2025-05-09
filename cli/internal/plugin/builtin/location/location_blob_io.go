package location

import (
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
)

func Write(location types.Location, b blob.ReadOnlyBlob) error {
	switch location.LocationType {
	case types.LocationTypeLocalFile:
		f, err := os.OpenFile(location.Value, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
		if err != nil {
			return fmt.Errorf("error opening file %q: %w", location.Value, err)
		}
		defer f.Close()
		if err := blob.Copy(f, b); err != nil {
			return fmt.Errorf("error copying blob to file %q: %w", location.Value, err)
		}
	case types.LocationTypeUnixNamedPipe:
		f, err := os.OpenFile(location.Value, os.O_WRONLY|os.O_APPEND|os.O_CREATE, os.ModeNamedPipe)
		if err != nil {
			return fmt.Errorf("error opening named pipe %q: %w", location.Value, err)
		}
		defer f.Close()
		if err := blob.Copy(f, b); err != nil {
			return fmt.Errorf("error copying blob to named pipe %q: %w", location.Value, err)
		}
	default:
		return fmt.Errorf("unsupported target location type %q", location.LocationType)
	}
	return nil
}

func Read(location types.Location) (blob.ReadOnlyBlob, error) {
	var b blob.ReadOnlyBlob
	var err error
	switch location.LocationType {
	case types.LocationTypeLocalFile, types.LocationTypeUnixNamedPipe:
		if b, err = filesystem.GetBlobFromOSPath(location.Value); err != nil {
			return nil, fmt.Errorf("error getting blob from OS path: %w", err)
		}
	default:
		return nil, fmt.Errorf("unsupported resource location type %q", location.LocationType)
	}
	return b, nil
}
