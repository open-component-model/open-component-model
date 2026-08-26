// Package input implements the constructor input method for the S3Bucket type. It
// downloads a single object from an S3 or S3-compatible bucket while OCM constructs a
// component version. It gives the object to the constructor as a local blob, so the
// finished component version holds the content, not a reference to the bucket.
package input

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
	identityv1 "ocm.software/open-component-model/bindings/go/s3/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/s3/spec/input"
	"ocm.software/open-component-model/bindings/go/s3/spec/input/v1"
)

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements the [constructor.ResourceInputMethod] interface for S3-based
// inputs.
type InputMethod struct {
	// HTTPConfig configures the HTTP client that the S3 client sends its requests
	// through. Its retry section also sets the attempt count of the SDK. Nil uses the
	// defaults of the shared client.
	HTTPConfig *httpv1alpha1.Config
	// MaxDownloadSize caps the number of bytes read from an object. Nil uses the
	// download package default, which is unlimited.
	MaxDownloadSize *int64
	// TempFolder is the directory that the package streams the downloaded object into.
	// When it is empty, the package uses the OS temporary directory. The file behind the
	// returned blob is created here and outlives ProcessResource, because it holds the
	// content that the constructor stores as a local blob.
	TempFolder string
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for
// an S3 input. It derives the same identity as the S3Bucket access type, so one
// consumer entry for a bucket resolves for both.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	spec, err := i.convertInput(resource)
	if err != nil {
		return nil, err
	}

	return identityv1.IdentityFromObject(spec.BucketName, spec.ObjectKey, spec.Endpoint)
}

// ProcessResource downloads the object of the S3Bucket input specification and returns
// it as local blob data. The constructor stores that data in the component version.
//
// The package streams the object into a file under [InputMethod.TempFolder]. The
// returned blob reads from that file. The file outlives this call, and the caller owns
// it.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	spec, err := i.convertInput(resource)
	if err != nil {
		return nil, err
	}

	opts := []download.Option{
		download.WithCredentials(credentials),
		download.WithTempDir(i.TempFolder),
	}
	if i.MaxDownloadSize != nil {
		opts = append(opts, download.WithMaxDownloadSize(*i.MaxDownloadSize))
	}
	if i.HTTPConfig != nil {
		opts = append(opts, download.WithHTTPConfig(i.HTTPConfig))
	}

	result, err := download.Download(ctx, download.Request{
		Region:       spec.Region,
		BucketName:   spec.BucketName,
		ObjectKey:    spec.ObjectKey,
		MediaType:    spec.MediaType,
		Version:      spec.Version,
		Endpoint:     spec.Endpoint,
		UsePathStyle: spec.UsePathStyle,
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("error downloading s3 input from %s: %w", spec.String(), err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: result.Blob,
	}, nil
}

func (i *InputMethod) convertInput(resource *constructorruntime.Resource) (*v1.S3Bucket, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Input == nil {
		return nil, fmt.Errorf("resource input is required")
	}

	spec := &v1.S3Bucket{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, spec); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid s3 input spec: %w", err)
	}

	return spec, nil
}
