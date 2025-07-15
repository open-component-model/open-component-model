package artifactrepository

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceRepository defines the interface for storing and retrieving OCM resources
// independently of component versions from a store implementation
type ResourceRepository interface {
	// UploadResource uploads a [descriptor.Resource] to the repository.
	// Returns the updated resource with repository-specific information.
	// The resource must be referenced in the component descriptor.
	UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (resourceAfterUpload *descriptor.Resource, err error)

	// DownloadResource downloads a [descriptor.Resource] from the repository.
	DownloadResource(ctx context.Context, res *descriptor.Resource) (content blob.ReadOnlyBlob, err error)
}

type SourceRepository interface {
	// UploadSource uploads a [descriptor.Source] to the repository.
	// Returns the updated source with repository-specific information.
	// The source must be referenced in the component descriptor.
	UploadSource(ctx context.Context, targetAccess runtime.Typed, source *descriptor.Source, content blob.ReadOnlyBlob) (sourceAfterUpload *descriptor.Source, err error)

	// DownloadSource downloads a [descriptor.Source] from the repository.
	DownloadSource(ctx context.Context, res *descriptor.Source) (content blob.ReadOnlyBlob, err error)
}
