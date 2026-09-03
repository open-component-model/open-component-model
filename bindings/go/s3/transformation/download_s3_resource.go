package transformation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/transformation/spec/v1alpha1"
)

// DownloadS3Resource is a transformer that downloads S3 resources from their bucket and buffers
// them to a file. The downloaded content is a plain blob; a subsequent AddLocalResource
// transformation is expected to embed it as a local blob in the target repository.
type DownloadS3Resource struct {
	Scheme *runtime.Scheme
	// ResourceRepository is used to download S3 resources and resolve credential consumer identities.
	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *DownloadS3Resource) Transform(ctx context.Context, step runtime.Typed) (_ runtime.Typed, err error) {
	var transformation v1alpha1.DownloadS3Resource
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to DownloadS3Resource transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for DownloadS3Resource transformation")
	}
	if transformation.Spec.Resource == nil {
		return nil, fmt.Errorf("resource is required in spec for DownloadS3Resource transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.DownloadS3ResourceOutput{}
	}

	outputPath, err := determineOutputPath(transformation.Spec.OutputPath, "s3")
	if err != nil {
		return nil, fmt.Errorf("error getting output path: %w", err)
	}
	// determineOutputPath creates the file; retain it on success for Output.File, remove it on failure.
	defer func() {
		if err != nil {
			if removeErr := os.Remove(outputPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				slog.WarnContext(ctx, "failed to clean up s3 output file after error", "path", outputPath, "err", removeErr)
			}
		}
	}()
	slog.DebugContext(ctx, "Going to use s3 output path", "path", outputPath)

	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)

	creds, err := t.resolveCredentials(ctx, targetResource)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Getting s3 resource", "resource", transformation.Spec.Resource)

	downloadedBlob, err := t.ResourceRepository.DownloadResource(ctx, targetResource, creds)
	if err != nil {
		return nil, fmt.Errorf("error downloading s3 resource: %w", err)
	}
	// The downloaded blob is backed by a temporary file the repository hands over to
	// us. Its content is copied to outputPath below, so release it afterwards instead
	// of leaving a second copy behind.
	defer func() {
		if closer, ok := downloadedBlob.(io.Closer); ok {
			if closeErr := closer.Close(); closeErr != nil {
				slog.WarnContext(ctx, "failed to remove temporary s3 download file", "err", closeErr)
			}
		}
	}()

	fileSpec, err := filesystem.BlobToSpec(downloadedBlob, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering downloaded blob to file: %w", err)
	}
	slog.DebugContext(ctx, "Converted downloaded blob to file spec", "uri", fileSpec.URI)

	v2Resource, err := descriptor.ConvertToV2Resource(t.Scheme, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	transformation.Output.File = *fileSpec
	transformation.Output.Resource = v2Resource

	return &transformation, nil
}

// resolveCredentials returns credentials for downloading targetResource, or nil if
// no credential provider is configured or the resource has no consumer identity.
// An ErrNotFound from the resolver is treated as "no credentials" rather than an error.
func (t *DownloadS3Resource) resolveCredentials(ctx context.Context, targetResource *descriptor.Resource) (runtime.Typed, error) {
	if t.CredentialProvider == nil {
		return nil, nil
	}
	consumerId, err := t.ResourceRepository.GetResourceCredentialConsumerIdentity(ctx, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed getting resource consumer identity for credential resolution: %w", err)
	}
	if consumerId == nil {
		return nil, nil
	}
	typed, err := t.CredentialProvider.Resolve(ctx, consumerId)
	if errors.Is(err, credentials.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed resolving credentials for consumer identity %v: %w", consumerId, err)
	}
	return typed, nil
}
