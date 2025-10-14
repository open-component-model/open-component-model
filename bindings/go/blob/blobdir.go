package blob

import (
	"context"
	"fmt"
)

const DefaultTarMediaType = "application/x-tar"

func (o *DirOptions) mediaType() string {
	if o.MediaType == "" {
		return DefaultTarMediaType
	}
	return o.MediaType
}

// GetBlobFromDir creates a blob from a directory using the provided options.
func GetBlobFromDir(ctx context.Context, opt DirOptions) (ReadOnlyBlob, error) {
	path := opt.Path
	if path == "" {
		return nil, fmt.Errorf("path must not be empty")
	}
}
