package input

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
	"ocm.software/open-component-model/bindings/go/s3/internal/identity"
	"ocm.software/open-component-model/bindings/go/s3/spec/input"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/input/v1"
)

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements the [constructor.ResourceInputMethod] interface for S3-based
// inputs. It downloads a single object from an S3 or S3-compatible bucket declared in
// the component constructor and returns it as a local blob to be stored in the
// component version.
type InputMethod struct {
	// Client optionally injects a pre-built S3 client (or a fake, in tests). When nil,
	// a client is constructed per download from the input spec and credentials.
	Client download.ObjectGetter
	// MaxDownloadSize caps the number of bytes read from an object. Nil uses the
	// download package default.
	MaxDownloadSize *int64
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for
// an S3 input. It uses the same S3Bucket consumer identity as the access type so that
// credentials configured for a bucket resolve for both.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	spec := v1.S3{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if err := spec.Validate(); err != nil {
		return nil, err
	}

	id, err := identity.Consumer(spec.Endpoint, spec.BucketName, spec.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("error building s3 consumer identity: %w", err)
	}

	return id, nil
}

// ProcessResource downloads the object described by the S3 input specification and
// returns it as local blob data to be stored in the component version.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	spec := v1.S3{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, &spec); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	if err := spec.Validate(); err != nil {
		return nil, err
	}

	opts := []download.Option{download.WithCredentials(credentials)}
	if i.Client != nil {
		opts = append(opts, download.WithClient(i.Client))
	}
	if i.MaxDownloadSize != nil {
		opts = append(opts, download.WithMaxDownloadSize(*i.MaxDownloadSize))
	}

	data, err := download.Download(ctx, download.Request{
		Region:                spec.Region,
		BucketName:            spec.BucketName,
		ObjectKey:             spec.ObjectKey,
		MediaType:             spec.MediaType,
		Version:               spec.Version,
		Endpoint:              spec.Endpoint,
		UsePathStyle:          spec.UsePathStyle,
		InsecureSkipTLSVerify: spec.InsecureSkipTLSVerify,
	}, opts...)
	if err != nil {
		return nil, fmt.Errorf("error downloading s3 input from %s: %w", spec.String(), err)
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: data,
	}, nil
}
