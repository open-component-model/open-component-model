package dir

import (
	"context"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrDirsDoNotRequireCredentials is returned when credential-related operations are attempted
// on directory inputs, since directories are accessed directly from the local filesystem and do not
// require authentication or authorization.
var ErrDirsDoNotRequireCredentials = fmt.Errorf("directories do not require credentials")

var _ interface {
	constructor.ResourceInputMethod
	constructor.SourceInputMethod
} = (*InputMethod)(nil)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&v1.Dir{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
	)
}

// InputMethod implements the ResourceInputMethod and SourceInputMethod interfaces
// for dir-based inputs. It provides functionality to process directories from the local
// filesystem as either resources or sources in the OCM constructor system.
//
// The InputMethod handles:
//   - Converting input specifications to v1.Dir format
//   - Reading directories from the filesystem
//   - Processing directory metadata and content
//   - Returning processed blob data for further use
//
// Since directories are accessed directly from the local filesystem, no credentials
// are required for any operations.
type InputMethod struct {
	// WorkingDirectory is the base directory used to resolve relative paths in input specifications.
	// If a path in the input specification is relative, it will be resolved against this directory.
	WorkingDirectory string
}

// NewInputMethod creates a new InputMethod instance with the specified working directory.
// The working directory is used to resolve relative paths in input specifications.
// If the working directory is empty, it defaults to the current working directory of the process.
func NewInputMethod(workingDir string) (*InputMethod, error) {
	if workingDir == "" {
		if wg, err := os.Getwd(); err != nil {
			return nil, fmt.Errorf("error getting current working directory: %w", err)
		} else {
			workingDir = wg
		}
	}

	return &InputMethod{
		WorkingDirectory: workingDir,
	}, nil
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return Scheme
}

// GetResourceCredentialConsumerIdentity returns nil identity and ErrDirsDoNotRequireCredentials
// since directory inputs do not require any credentials for access. Directories are read directly
// from the local filesystem without authentication.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Resource) (identity runtime.Identity, err error) {
	return nil, ErrDirsDoNotRequireCredentials
}

// ProcessResource processes a dir-based resource input by converting the input specification
// to a v1.Dir format, reading the directory from the filesystem, and returning the processed
// blob data. This method handles optional compression and other operations
// as specified in the input configuration.
//
// The method performs the following steps:
//  1. Converts the resource input to v1.Dir specification
//  2. Calls GetV1DirBlob to read and process the directory
//  3. Returns the processed blob data wrapped in a ResourceInputMethodResult
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, _ map[string]string) (result *constructor.ResourceInputMethodResult, err error) {
	dir := v1.Dir{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &dir); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	dirBlob, err := GetV1DirBlob(ctx, dir, i.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("error getting dir blob based on resource input specification: %w", err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: dirBlob,
	}, nil
}

// GetSourceCredentialConsumerIdentity returns nil identity and ErrDirsDoNotRequireCredentials
// since directory inputs do not require any credentials for access. Directories are read directly
// from the local filesystem without authentication.
func (i *InputMethod) GetSourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Source) (identity runtime.Identity, err error) {
	return nil, ErrDirsDoNotRequireCredentials
}

// ProcessSource processes a dir-based source input by converting the input specification
// to a v1.Dir format, reading the directory from the filesystem, and returning the processed
// blob data. This method handles optional compression and other operations
// as specified in the input configuration.
//
// The method performs the following steps:
//  1. Converts the source input to v1.Dir specification
//  2. Calls GetV1DirBlob to read and process the directory
//  3. Returns the processed blob data wrapped in a SourceInputMethodResult
func (i *InputMethod) ProcessSource(ctx context.Context, src *constructorruntime.Source, _ map[string]string) (result *constructor.SourceInputMethodResult, err error) {
	dir := v1.Dir{}
	if err := i.GetInputMethodScheme().Convert(src.Input, &dir); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := GetV1DirBlob(ctx, dir, i.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("error getting dir blob based on source input specification: %w", err)
	}

	return &constructor.SourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}
