package v1

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/plugin/manager/registries/input"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	_ constructor.ResourceInputMethod = &ResourceInputMethod{}
	_ constructor.SourceInputMethod   = &SourceInputMethod{}
)

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterWithAlias(&File{}, runtime.NewVersionedType(Type, Version), runtime.NewUnversionedType(Type))
}

func Register(inputRegistry *input.RepositoryRegistry) error {
	if err := input.RegisterInternalResourceInputPlugin(scheme, inputRegistry, &ResourceInputMethod{}, &File{}); err != nil {
		return fmt.Errorf("could not register file resource input method: %w", err)
	}
	if err := input.RegisterInternalSourcePlugin(scheme, inputRegistry, &SourceInputMethod{}, &File{}); err != nil {
		return fmt.Errorf("could not register file source input method: %w", err)
	}

	return nil
}

type ResourceInputMethod struct{}

func (i *ResourceInputMethod) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	return nil, fmt.Errorf("files do not require credentials")
}

func (i *ResourceInputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, _ map[string]string) (result *constructor.ResourceInputMethodResult, err error) {
	file := File{}
	if err := scheme.Convert(resource.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := getFileBlob(file)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob: %w", err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}

type SourceInputMethod struct {
}

func (i *SourceInputMethod) GetSourceCredentialConsumerIdentity(ctx context.Context, source *constructorruntime.Source) (identity runtime.Identity, err error) {
	return nil, fmt.Errorf("files do not require credentials")
}

func (i *SourceInputMethod) ProcessSource(ctx context.Context, src *constructorruntime.Source, _ map[string]string) (result *constructor.SourceInputMethodResult, err error) {
	file := File{}
	if err := scheme.Convert(src.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := getFileBlob(file)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob: %w", err)
	}

	return &constructor.SourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}

func getFileBlob(file File) (blob.ReadOnlyBlob, error) {
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
