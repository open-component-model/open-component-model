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
	inputSpec "ocm.software/open-component-model/bindings/go/constructor/spec/input"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ input.Method = &Method{}

type Method struct{}

func (i *Method) ProcessResource(_ context.Context, resource *spec.Resource) (data blob.ReadOnlyBlob, err error) {
	return i.process(resource.Input, err, data)
}

func (i *Method) process(input runtime.Typed, err error, data blob.ReadOnlyBlob) (blob.ReadOnlyBlob, error) {
	file := v1.File{}
	if err := inputSpec.Scheme.Convert(input, &file); err != nil {
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

	data = &InputFileBlob{b, mediaType}

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
