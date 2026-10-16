package repository

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/internal/download"
	v2 "ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
)

// newStream builds the lazy blob for the S3 object. No request is sent.
func (r *ResourceRepository) newStream(ctx context.Context, spec *v2.S3, credentials runtime.Typed) *download.Stream {
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

	return download.NewStream(ctx, download.Request{
		Region:       spec.Region,
		BucketName:   spec.BucketName,
		ObjectKey:    spec.ObjectKey,
		MediaType:    spec.MediaType,
		Version:      spec.Version,
		Endpoint:     spec.Endpoint,
		UsePathStyle: spec.UsePathStyle,
	}, opts...)
}

// streamResource wraps stream so that the content is verified against the
// resource digest as it is read. When the access does not pin a media type, the
// object is requested once here to read it from the response; the first read of
// the returned blob uses that response, so no second request is sent.
func streamResource(ctx context.Context, resource *descriptor.Resource, stream *download.Stream) (blob.ReadOnlyBlob, error) {
	verified, err := repository.VerifyDownload(ctx, resource, stream)
	if err != nil {
		return nil, fmt.Errorf("failed preparing s3 resource stream for verification: %w", err)
	}
	if _, known := stream.MediaType(); !known {
		if err := stream.Prime(); err != nil {
			_ = stream.Close()
			return nil, err
		}
	}
	return verified, nil
}
