package file

import (
	"context"
	"fmt"

	"github.com/gabriel-vasile/mimetype"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor"
	v1 "ocm.software/open-component-model/bindings/go/constructor/input/file/spec/v1"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	_ interface {
		constructor.ResourceInputMethod
		constructor.SourceInputMethod
	} = (*InputMethod)(nil)
)

var scheme = runtime.NewScheme()

func init() {
	scheme.MustRegisterWithAlias(&v1.File{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
	)
}

type InputMethod struct{}

func (i *InputMethod) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	return nil, fmt.Errorf("files do not require credentials")
}

func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, _ map[string]string) (result *constructor.ResourceInputMethodResult, err error) {
	file := v1.File{}
	if err := scheme.Convert(resource.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := GetV1FileBlob(file)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob: %w", err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}

func (i *InputMethod) GetSourceCredentialConsumerIdentity(ctx context.Context, source *constructorruntime.Source) (identity runtime.Identity, err error) {
	return nil, fmt.Errorf("files do not require credentials")
}

func (i *InputMethod) ProcessSource(ctx context.Context, src *constructorruntime.Source, _ map[string]string) (result *constructor.SourceInputMethodResult, err error) {
	file := v1.File{}
	if err := scheme.Convert(src.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := GetV1FileBlob(file)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob: %w", err)
	}

	return &constructor.SourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}

func GetV1FileBlob(file v1.File) (blob.ReadOnlyBlob, error) {
	b, err := filesystem.GetBlobFromOSPath(file.Path)
	if err != nil {
		return nil, err
	}

	mediaType := file.MediaType
	if mediaType == "" {
		// see https://github.com/gabriel-vasile/mimetype/blob/master/supported_mimes.md for supported types
		mime, _ := mimetype.DetectFile(file.Path)
		mediaType = mime.String()
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

var _ interface {
	blob.MediaTypeAware
	blob.SizeAware
	blob.DigestAware
} = (*InputFileBlob)(nil)
