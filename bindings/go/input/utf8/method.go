package utf8

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/compression"
	"ocm.software/open-component-model/bindings/go/blob/direct"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/input/utf8/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ErrUTF8StringsDoNotRequireCredentials is returned when credential-related operations are attempted
// on utf8 inputs, since files are accessed directly from the local filesystem and do not
// require authentication or authorization.
var ErrUTF8StringsDoNotRequireCredentials = fmt.Errorf("UTF8 strings do not require credentials")

var _ interface {
	constructor.ResourceInputMethod
	constructor.SourceInputMethod
} = (*InputMethod)(nil)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&v1.UTF8{},
		runtime.NewVersionedType(v1.Type, v1.Version),
		runtime.NewUnversionedType(v1.Type),
	)
}

// InputMethod implements the ResourceInputMethod and SourceInputMethod interfaces
// for utf8-based inputs. It provides functionality to process files from the local
// constructor as either resources or sources in the OCM constructor system.
//
// The InputMethod handles:
//   - Converting input specifications to v1.UTF8 format
//   - Reading the utf8 string
//   - Processing file metadata and content
//   - Returning processed blob data for further use
type InputMethod struct{}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return Scheme
}

// GetResourceCredentialConsumerIdentity returns nil identity and ErrFilesDoNotRequireCredentials
// since file inputs do not require any credentials for access. Files are read directly
// from the local filesystem without authentication.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (identity runtime.Identity, err error) {
	return nil, ErrUTF8StringsDoNotRequireCredentials
}

// ProcessResource processes a UTF8-based resource input by converting the input specification
// to a v1.UTF8 format, reading the utf8 string, and returning the processed blob data based on GetV1UTF8Blob.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, _ map[string]string) (result *constructor.ResourceInputMethodResult, err error) {
	utf8 := v1.UTF8{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &utf8); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	utf8Blob, err := GetV1UTF8Blob(utf8)
	if err != nil {
		return nil, fmt.Errorf("error getting utf8 blob based on resource input specification: %w", err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: utf8Blob,
	}, nil
}

// GetSourceCredentialConsumerIdentity returns nil identity and ErrFilesDoNotRequireCredentials
// since file inputs do not require any credentials for access. Files are read directly
// from the local filesystem without authentication.
func (i *InputMethod) GetSourceCredentialConsumerIdentity(_ context.Context, source *constructorruntime.Source) (identity runtime.Identity, err error) {
	return nil, ErrUTF8StringsDoNotRequireCredentials
}

// ProcessSource processes a UTF8-based source input by converting the input specification
// to a v1.UTF8 format and returning the processed blob data based on GetV1UTF8Blob.
func (i *InputMethod) ProcessSource(_ context.Context, src *constructorruntime.Source, _ map[string]string) (result *constructor.SourceInputMethodResult, err error) {
	utf8 := v1.UTF8{}
	if err := i.GetInputMethodScheme().Convert(src.Input, &utf8); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := GetV1UTF8Blob(utf8)
	if err != nil {
		return nil, fmt.Errorf("error getting utf8 blob based on source input specification: %w", err)
	}

	return &constructor.SourceInputMethodResult{
		ProcessedBlobData: fileBlob,
	}, nil
}

func GetV1UTF8Blob(utf8 v1.UTF8) (blob.ReadOnlyBlob, error) {
	if err := utf8.Validate(); err != nil {
		return nil, fmt.Errorf("error validating utf8 input spec: %w", err)
	}

	var (
		reader    io.Reader
		mediaType string
		size      int64
	)

	switch {
	case utf8.Text != "":
		reader = strings.NewReader(utf8.Text)
		mediaType = "text/plain"
		size = int64(len(utf8.Text))
	case len(utf8.JSON) > 0, len(utf8.FormattedJSON) > 0:
		var data []byte
		var err error
		switch {
		case len(utf8.JSON) > 0:
			data, err = json.Marshal(utf8.JSON)
		case len(utf8.FormattedJSON) > 0:
			data, err = json.MarshalIndent(utf8.FormattedJSON, "", "  ")
		}
		if err != nil {
			return nil, fmt.Errorf("error marshalling utf8 JSON input: %w", err)
		}
		reader = bytes.NewReader(data)
		mediaType = "application/json"
		size = int64(len(data))
	case len(utf8.YAML) > 0:
		data, err := yaml.Marshal(utf8.YAML)
		if err != nil {
			return nil, fmt.Errorf("error marshalling utf8 YAML input: %w", err)
		}
		reader = bytes.NewReader(data)
		mediaType = "application/x-yaml"
		size = int64(len(data))
	default:
		return nil, fmt.Errorf("utf8 input must contain a valid content description")
	}

	var utf8Blob blob.ReadOnlyBlob = direct.New(reader,
		direct.WithMediaType(mediaType),
		direct.WithSize(size),
	)

	if utf8.Compress {
		utf8Blob = compression.Compress(utf8Blob)
	}

	return utf8Blob, nil
}
