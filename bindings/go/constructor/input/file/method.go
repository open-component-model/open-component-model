package file

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/constructor"
	v1 "ocm.software/open-component-model/bindings/go/constructor/input/file/spec/v1"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var ErrFilesDoNotRequireCredentials = fmt.Errorf("files do not require credentials")

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

func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	return nil, ErrFilesDoNotRequireCredentials
}

func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, _ map[string]string) (result *constructor.ResourceInputMethodResult, err error) {
	file := v1.File{}
	if err := scheme.Convert(resource.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := GetV1FileBlob(file)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob based on resource input specification: %w", err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}

func (i *InputMethod) GetSourceCredentialConsumerIdentity(_ context.Context, source *constructorruntime.Source) (identity runtime.Identity, err error) {
	return nil, ErrFilesDoNotRequireCredentials
}

func (i *InputMethod) ProcessSource(_ context.Context, src *constructorruntime.Source, _ map[string]string) (result *constructor.SourceInputMethodResult, err error) {
	file := v1.File{}
	if err := scheme.Convert(src.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := GetV1FileBlob(file)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob based on source input specification: %w", err)
	}

	return &constructor.SourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}
