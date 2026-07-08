package repository

import (
	"context"
	"fmt"
	"net/http"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
	"ocm.software/open-component-model/bindings/go/wget/internal/identity"
	accessspec "ocm.software/open-component-model/bindings/go/wget/spec/access"
	"ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
)

var _ repository.ResourceRepository = (*ResourceRepository)(nil)

// ResourceRepository implements the ResourceRepository interface for wget access types.
type ResourceRepository struct {
	client          *http.Client
	maxDownloadSize int64
}

// NewResourceRepository creates a new wget resource repository.
func NewResourceRepository(opts ...Option) *ResourceRepository {
	options := &Options{}
	for _, opt := range opts {
		opt(options)
	}
	client := options.Client
	if client == nil {
		client = http.DefaultClient
	}
	var maxSize int64
	if options.MaxDownloadSize != nil {
		maxSize = *options.MaxDownloadSize
	} else {
		maxSize = DefaultMaxDownloadSize
	}
	return &ResourceRepository{
		client:          client,
		maxDownloadSize: maxSize,
	}
}

// GetResourceRepositoryScheme returns the scheme used by the wget resource repository.
func (r *ResourceRepository) GetResourceRepositoryScheme() *runtime.Scheme {
	return accessspec.Scheme
}

// GetResourceCredentialConsumerIdentity resolves the credential consumer identity for the given resource.
func (r *ResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	wget := v1.Wget{}
	if err := r.GetResourceRepositoryScheme().Convert(resource.Access, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	return identity.CredentialConsumerIdentity(wget.URL)
}

// DownloadResource downloads a resource from the URL specified in the wget access spec.
func (r *ResourceRepository) DownloadResource(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if resource.Access == nil {
		return nil, fmt.Errorf("resource access is required")
	}

	wget := v1.Wget{}
	if err := accessspec.Scheme.Convert(resource.Access, &wget); err != nil {
		return nil, fmt.Errorf("error converting resource access spec: %w", err)
	}

	return download.Download(ctx, download.Request{
		URL:        wget.URL,
		MediaType:  wget.MediaType,
		Header:     wget.Header,
		Verb:       wget.Verb,
		Body:       wget.Body,
		NoRedirect: wget.NoRedirect,
	},
		download.WithClient(r.client),
		download.WithMaxDownloadSize(r.maxDownloadSize),
		download.WithCredentials(credentials),
	)
}

// UploadResource is not supported for wget access types.
func (r *ResourceRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return nil, fmt.Errorf("upload is not supported for wget access type")
}
