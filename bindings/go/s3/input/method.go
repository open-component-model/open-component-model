// Package input implements the constructor input method for the S3 type. It
// downloads a single object from an S3 or S3-compatible bucket while a component
// version is constructed and hands it to the constructor as a local blob, so the
// finished component version carries the content instead of a reference to the bucket.
package input

import (
	"context"
	"fmt"
	"io"

	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob/verified"
	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
	s3creds "ocm.software/open-component-model/bindings/go/s3/spec/credentials"
	identityv1 "ocm.software/open-component-model/bindings/go/s3/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/s3/spec/input"
	"ocm.software/open-component-model/bindings/go/s3/spec/input/v2"
)

var _ constructor.ResourceInputMethod = (*InputMethod)(nil)

// InputMethod implements the [constructor.ResourceInputMethod] interface for S3-based
// inputs.
type InputMethod struct {
	// HTTPConfig configures the HTTP client the S3 client sends its requests through,
	// and its retry section additionally drives the SDK's attempt count. Nil uses the
	// shared client's defaults.
	HTTPConfig *httpv1alpha1.Config
	// MaxDownloadSize caps the number of bytes read from an object. Nil uses the
	// download package default, which is unlimited.
	MaxDownloadSize *int64
}

func (i *InputMethod) GetInputMethodScheme() *runtime.Scheme {
	return input.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for
// an S3 input. It derives the same identity as the S3 access type, so a consumer
// entry configured for a bucket resolves for both.
func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, resource *constructorruntime.Resource) (runtime.Identity, error) {
	spec, err := i.convertInput(resource)
	if err != nil {
		return nil, err
	}

	return identityv1.IdentityFromObject(spec.BucketName, spec.ObjectKey, spec.Endpoint)
}

// ProcessResource returns the object described by the S3 input specification as
// local blob data to be stored in the component version.
//
// The object is read once here to learn its version, digest, size and media type, and
// again whenever the returned blob is read; nothing is stored. If the store reported a
// real object version, later reads are pinned to it. Every read is verified against
// the digest and size of the first, so an object overwritten in an unversioned bucket
// fails the read instead of being stored.
func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, credentials runtime.Typed) (*constructor.ResourceInputMethodResult, error) {
	spec, err := i.convertInput(resource)
	if err != nil {
		return nil, err
	}

	opts := []download.Option{download.WithCredentials(credentials)}
	if i.MaxDownloadSize != nil {
		opts = append(opts, download.WithMaxDownloadSize(*i.MaxDownloadSize))
	}
	if i.HTTPConfig != nil {
		opts = append(opts, download.WithHTTPConfig(i.HTTPConfig))
	}

	req := download.Request{
		Region:       spec.Region,
		BucketName:   spec.BucketName,
		ObjectKey:    spec.ObjectKey,
		MediaType:    spec.MediaType,
		Version:      spec.Version,
		Endpoint:     spec.Endpoint,
		UsePathStyle: spec.UsePathStyle,
	}
	result, err := download.Stream(ctx, req, io.Discard, append(opts, download.WithDigestAlgorithm(digest.SHA256))...)
	if err != nil {
		return nil, fmt.Errorf("error downloading s3 input from %s: %w", spec.String(), err)
	}
	if req.Version == "" && download.IsVersion(result.VersionID) {
		req.Version = result.VersionID
	}

	return &constructor.ResourceInputMethodResult{
		ProcessedBlobData: verified.New(download.NewBlob(ctx, req, opts...), verified.Metadata{
			Digest:    result.Digest,
			Size:      result.Size,
			MediaType: result.MediaType,
		}),
	}, nil
}

func (i *InputMethod) convertInput(resource *constructorruntime.Resource) (*v2.S3, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Input == nil {
		return nil, fmt.Errorf("resource input is required")
	}

	spec := &v2.S3{}
	if err := i.GetInputMethodScheme().Convert(resource.Input, spec); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("invalid s3 input spec: %w", err)
	}

	return spec, nil
}

func (i *InputMethod) GetCredentialTypeScheme() *runtime.Scheme {
	return s3creds.Scheme
}
