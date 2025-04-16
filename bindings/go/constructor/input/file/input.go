package file

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor/input"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	inputSpec "ocm.software/open-component-model/bindings/go/constructor/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/constructor/spec/input/v1"
)

var _ input.Method = &Method{}

type Method struct{}

func (i *Method) GetBlob(_ context.Context, resource *spec.Resource) (data blob.ReadOnlyBlob, err error) {
	file := v1.File{}
	if err := inputSpec.Scheme.Convert(resource.Input, &file); err != nil {
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

	if file.CompressWithGzip {
		data = NewCompressedBlob(data)
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
