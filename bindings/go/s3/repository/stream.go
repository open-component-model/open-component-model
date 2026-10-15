package repository

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
)

// Assert that the s3 resource repository offers the streaming capability.
var _ repository.StreamingResourceRepository = (*ResourceRepository)(nil)

// DownloadResourceStream returns a lazy, replayable blob for an S3 resource that
// streams the object body on demand instead of materializing it to a temporary
// file. It is the streaming counterpart of [ResourceRepository.DownloadResource]
// and backs the by-value transfer path to a remote target.
//
// The resource digest is verified inline, stream-then-verify: the lazy blob is
// wrapped with [repository.VerifyDownload], which checks the content against the
// descriptor digest as it is read and reports a mismatch at end-of-stream. A
// corrupted transfer therefore still fails even though the body has already been
// forwarded to the target.
func (r *ResourceRepository) DownloadResourceStream(ctx context.Context, resource *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	spec, err := r.convertAccess(resource)
	if err != nil {
		return nil, err
	}

	opts := []download.Option{
		download.WithCredentials(credentials),
	}
	if r.maxDownloadSize != nil {
		opts = append(opts, download.WithMaxDownloadSize(*r.maxDownloadSize))
	}
	if r.httpConfig != nil {
		opts = append(opts, download.WithHTTPConfig(r.httpConfig))
	}
	if r.httpClient != nil {
		opts = append(opts, download.WithHTTPClient(r.httpClient))
	}

	stream := download.NewStream(ctx, download.Request{
		Region:       spec.Region,
		BucketName:   spec.BucketName,
		ObjectKey:    spec.ObjectKey,
		MediaType:    spec.MediaType,
		Version:      spec.Version,
		Endpoint:     spec.Endpoint,
		UsePathStyle: spec.UsePathStyle,
	}, opts...)

	verified, err := repository.VerifyDownload(ctx, resource, stream)
	if err != nil {
		return nil, fmt.Errorf("failed preparing s3 resource stream for verification: %w", err)
	}
	return verified, nil
}
