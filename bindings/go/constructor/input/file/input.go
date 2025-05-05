package file

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor/input"
	"ocm.software/open-component-model/bindings/go/constructor/input/file/spec/v1"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ input.Method = &Method{}

type Method struct{ Scheme *runtime.Scheme }

func (i *Method) ProcessResource(_ context.Context, resource *spec.Resource) (blob.ReadOnlyBlob, error) {
	return i.process(resource.Input)
}

func (i *Method) process(input runtime.Typed) (blob.ReadOnlyBlob, error) {
	file := v1.File{}
	if err := i.Scheme.Convert(input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	b, err := filesystem.GetBlobFromOSPath(file.Path)
	if err != nil {
		return nil, err
	}

	mediaType := file.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	data := blob.ReadOnlyBlob(&InputFileBlob{b, mediaType})

	if file.Compress {
		data = compression.Compress(data)
	}

	return data, nil
}

type InputFileBlob struct {
	*filesystem.Blob
	mediaType string
}

func (i InputFileBlob) MediaType() (mediaType string, known bool) {
	return i.mediaType, i.mediaType != ""
}

var _ blob.MediaTypeAware = (*InputFileBlob)(nil)
