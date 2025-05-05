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
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ input.ResourceInputMethod = &Method{}

type Method struct{ Scheme *runtime.Scheme }

func (i *Method) ProcessResource(ctx context.Context, resource *spec.Resource, opts input.Options) (processed *descriptor.Resource, err error) {
	return i.process(ctx, resource, opts)
}

func (i *Method) process(ctx context.Context, resource *spec.Resource, opts input.Options) (processed *descriptor.Resource, err error) {
	file := v1.File{}
	if err := i.Scheme.Convert(resource.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := getFileBlob(file)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob: %w", err)
	}

	return input.AddColocatedLocalBlob(ctx, opts.Target, opts.Component, opts.Version, resource, fileBlob)
}

func getFileBlob(file v1.File) (blob.ReadOnlyBlob, error) {
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

// InputFileBlob wraps a blob and provides an additional media type for interpretation of the file content.
type InputFileBlob struct {
	*filesystem.Blob
	mediaType string
}

func (i InputFileBlob) MediaType() (mediaType string, known bool) {
	return i.mediaType, i.mediaType != ""
}

var _ blob.MediaTypeAware = (*InputFileBlob)(nil)
