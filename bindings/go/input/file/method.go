package file

import (
	"context"
	"fmt"
	"os"
	goPath "path"

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
	WorkingDirectory string
}

// GetResourceCredentialConsumerIdentity returns nil identity and ErrFilesDoNotRequireCredentials
// since file inputs do not require any credentials for access. Files are read directly
// from the local filesystem without authentication.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
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
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, _ map[string]string) (result *constructor.ResourceInputMethodResult, err error) {
	file := v1.File{}
	if err := Scheme.Convert(resource.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if err := ensureAbsolutePath(&file, i.WorkingDirectory); err != nil {
		return nil, fmt.Errorf("error ensuring absolute path for file: %w", err)
	}

	fileBlob, err := GetV1FileBlob(file)
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
func (i *InputMethod) GetSourceCredentialConsumerIdentity(_ context.Context, source *constructorruntime.Source) (identity runtime.Identity, err error) {
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
	if err := Scheme.Convert(src.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if err := ensureAbsolutePath(&file, i.WorkingDirectory); err != nil {
		return nil, fmt.Errorf("error ensuring absolute path for file: %w", err)
	}

	fileBlob, err := GetV1FileBlob(file)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob based on source input specification: %w", err)
	}

	return &constructor.SourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}

// EnsureAbsolutePath checks if the provided path is absolute. If it is not,
// it prepends the working directory to the path to make it absolute.
// If the working directory is not provided, it uses the current working directory.
// The function modifies the path in place and returns an error if it fails to get the current working directory.
func ensureAbsolutePath(file *v1.File, workingDir string) error {
	if goPath.IsAbs(file.Path) {
		return nil
	}

	if workingDir == "" {
		if dir, err := os.Getwd(); err != nil {
			return fmt.Errorf("error getting current working directory: %w", err)
		} else {
			workingDir = dir
		}
	}
	// make sure that we do not have two slashes in the path
	file.Path = goPath.Clean(workingDir + "/" + file.Path)

	return nil
}

func (i *InputMethod) GetWd() string {
	return i.WorkingDirectory
}

func (i *InputMethod) SetWd(workingDir string) {
	i.WorkingDirectory = workingDir
}
