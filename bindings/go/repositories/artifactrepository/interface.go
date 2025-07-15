package artifactrepository

import (
	"context"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ResourceRepository defines the interface for storing and retrieving OCM resources
// independently of component versions from a Store Implementation
type ResourceRepository interface {
	// UploadResource uploads a [descriptor.Resource] to the repository.
	// Returns the updated resource with repository-specific information.
	// The resource must be referenced in the component descriptor.
	// Note that UploadResource is special in that it considers both
	// - the Access from [descriptor.Resource.Access]
	// - the Target Access from the given target specification
	// It might be that during the upload, the source pointer may be updated with information gathered during upload
	// (e.g. digest, size, etc).
	//
	// The content of form blob.ReadOnlyBlob is expected to be a (optionally gzipped) tar archive that can be read with
	// tar.ReadOCILayout, which interprets the blob as an OCILayout.
	//
	// The given OCI Layout MUST contain the resource described in source with an v1.OCIImage specification,
	// otherwise the upload will fail
	UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob) (resourceAfterUpload *descriptor.Resource, err error)

	// DownloadResource downloads a [descriptor.Resource] from the repository.
	// THe resource MUST contain a valid v1.OCIImage specification that exists in the Store.
	// Otherwise, the download will fail.
	//
	// The blob.ReadOnlyBlob returned will always be an OCI Layout, readable by [tar.ReadOCILayout].
	// For more information on the download procedure, see [tar.NewOCILayoutWriter].
	DownloadResource(ctx context.Context, res *descriptor.Resource) (content blob.ReadOnlyBlob, err error)
}

type SourceRepository interface {
	// UploadSource uploads a [descriptor.Source] to the repository.
	// Returns the updated source with repository-specific information.
	// The source must be referenced in the component descriptor.
	// Note that UploadSource is special in that it considers both
	// - the Access from [descriptor.Source.Access]
	// - the Target Access from the given target specification
	// It might be that during the upload, the source pointer may be updated with information gathered during upload
	// (e.g. digest, size, etc).
	//
	// The content of form blob.ReadOnlyBlob is expected to be a (optionally gzipped) tar archive that can be read with
	// tar.ReadOCILayout, which interprets the blob as an OCILayout.
	//
	// The given OCI Layout MUST contain the source described in source with an v1.OCIImage specification,
	// otherwise the upload will fail
	UploadSource(ctx context.Context, targetAccess runtime.Typed, source *descriptor.Source, content blob.ReadOnlyBlob) (sourceAfterUpload *descriptor.Source, err error)

	// DownloadSource downloads a [descriptor.Source] from the repository.
	// THe resource MUST contain a valid v1.OCIImage specification that exists in the Store.
	// Otherwise, the download will fail.
	//
	// The blob.ReadOnlyBlob returned will always be an OCI Layout, readable by [tar.ReadOCILayout].
	// For more information on the download procedure, see [tar.NewOCILayoutWriter].
	DownloadSource(ctx context.Context, res *descriptor.Source) (content blob.ReadOnlyBlob, err error)
}
