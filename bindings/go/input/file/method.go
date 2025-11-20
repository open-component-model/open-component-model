package file

import (
	"context"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/input/file/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrFilesDoNotRequireCredentials is returned when credential-related operations are attempted
// on file inputs, since files are accessed directly from the local filesystem and do not
// require authentication or authorization.
var ErrFilesDoNotRequireCredentials = fmt.Errorf("files do not require credentials")

var _ interface {
	constructor.ResourceInputMethod
	constructor.SourceInputMethod
} = (*InputMethod)(nil)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&v1.File{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
	)
}

// InputMethod implements the ResourceInputMethod and SourceInputMethod interfaces
// for file-based inputs. It provides functionality to process files from the local
// filesystem as either resources or sources in the OCM constructor system.
//
// The InputMethod handles:
//   - Converting input specifications to v1.File format
//   - Reading files from the filesystem
//   - Processing file metadata and content
//   - Returning processed blob data for further use
//
// Since files are accessed directly from the local filesystem, no credentials
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
	var err error
	if workingDir == "" {
		workingDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("error getting current working directory: %w", err)
		}
	}

	return &InputMethod{
		WorkingDirectory: workingDir,
	}, nil
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return Scheme
}

// GetResourceCredentialConsumerIdentity returns nil identity and ErrFilesDoNotRequireCredentials
// since file inputs do not require any credentials for access. Files are read directly
// from the local filesystem without authentication.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Resource) (identity runtime.Identity, err error) {
	return nil, ErrFilesDoNotRequireCredentials
}

// ProcessResource processes a file-based resource input by converting the input specification
// to a v1.File format, reading the file from the filesystem, and returning the processed
// blob data. This method handles automatic media type detection and optional compression
// as specified in the file configuration.
//
// The method performs the following steps:
//  1. Converts the resource input to v1.File specification
//  2. Calls GetV1FileBlob to read and process the file
//  3. Returns the processed blob data wrapped in a ResourceInputMethodResult
func (i *InputMethod) ProcessResource(_ context.Context, resource *constructorruntime.Resource, _ map[string]string) (result *constructor.ResourceInputMethodResult, err error) {
	file := v1.File{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := GetV1FileBlob(file, i.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob based on resource input specification: %w", err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}

// GetSourceCredentialConsumerIdentity returns nil identity and ErrFilesDoNotRequireCredentials
// since file inputs do not require any credentials for access. Files are read directly
// from the local filesystem without authentication.
func (i *InputMethod) GetSourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Source) (identity runtime.Identity, err error) {
	return nil, ErrFilesDoNotRequireCredentials
}

// ProcessSource processes a file-based source input by converting the input specification
// to a v1.File format, reading the file from the filesystem, and returning the processed
// blob data. This method handles automatic media type detection and optional compression
// as specified in the file configuration.
//
// The method performs the following steps:
//  1. Converts the source input to v1.File specification
//  2. Calls GetV1FileBlob to read and process the file
//  3. Returns the processed blob data wrapped in a SourceInputMethodResult
func (i *InputMethod) ProcessSource(_ context.Context, src *constructorruntime.Source, _ map[string]string) (result *constructor.SourceInputMethodResult, err error) {
	file := v1.File{}
	if err := i.GetInputMethodScheme().Convert(src.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := GetV1FileBlob(file, i.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob based on source input specification: %w", err)
	}

	return &constructor.SourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}
