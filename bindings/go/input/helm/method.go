package helm

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/input/helm/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrHelmInputDoesNotRequireCredentials is returned when credential-related operations are attempted
// on helm inputs, since those are based on local filesystem and do not require authentication or authorization.
var ErrHelmInputDoesNotRequireCredentials = fmt.Errorf("helm inputs do not require credentials")

var _ interface {
	constructor.ResourceInputMethod
} = (*InputMethod)(nil)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&v1.Helm{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
	)
}

// InputMethod implements the ResourceInputMethod and SourceInputMethod interfaces for helm-based inputs.
// It provides functionality to process local filesystem directories, which have helm chart structure,
// as either resources or sources in the OCM constructor system.
//
// Since directories are accessed directly from the local filesystem, no credentials
// are required for any operations.
type InputMethod struct {
	TempDir string
}

// GetResourceCredentialConsumerIdentity returns nil identity and ErrHelmInputDoesNotRequireCredentials
// since helm inputs do not require any credentials for access. The data is read directly
// from the local filesystem without authentication.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	return nil, ErrHelmInputDoesNotRequireCredentials
}

// ProcessResource processes a helm-based resource input by converting the input specification
// to a v1.Helm format, reading the directory from the filesystem, and returning the processed
// blob data as an OCI artifact.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, _ map[string]string) (result *constructor.ResourceInputMethodResult, err error) {
	helm := v1.Helm{}
	if err := Scheme.Convert(resource.Input, &helm); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	helmBlob, err := GetV1HelmBlob(ctx, helm, i.TempDir)
	if err != nil {
		return nil, fmt.Errorf("error getting helm blob based on resource input specification: %w", err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: helmBlob,
	}, nil
}
