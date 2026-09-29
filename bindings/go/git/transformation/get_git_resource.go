// Package transformation provides transformers for Git resources.
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
	"ocm.software/open-component-model/bindings/go/git/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetGitResource downloads a Git repository snapshot as a gzipped tar archive.
// A subsequent AddLocalResource transformation can embed it in the target repository.
type GetGitResource struct {
	Scheme *runtime.Scheme
	// ResourceRepository downloads Git resources and resolves credential consumer identities.

	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *GetGitResource) Transform(ctx context.Context, step runtime.Typed) (_ runtime.Typed, err error) {
	var transformation v1alpha1.GetGitResource
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get git resource transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for get git resource transformation")
	}
	if transformation.Spec.Resource == nil {
		return nil, fmt.Errorf("resource is required in spec for get git resource transformation")
	}

	outputPath, err := determineOutputPath(transformation.Spec.OutputPath, "git-resource")
	if err != nil {
		return nil, fmt.Errorf("error getting content output path: %w", err)
	}
	// Failed graph nodes never pass their output to the consuming cleanup node.
	defer func() {
		if err != nil {
			if removeErr := os.Remove(outputPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				slog.WarnContext(ctx, "failed to clean up git output file after error", "path", outputPath, "err", removeErr)
			}
		}
	}()

	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)
	creds, err := t.resolveCredentials(ctx, targetResource)
	if err != nil {
		return nil, err
	}
	downloadedBlob, err := t.ResourceRepository.DownloadResource(ctx, targetResource, creds)
	if err != nil {
		return nil, fmt.Errorf("error downloading git resource: %w", err)
	}
	// The output is a separate copy; release the source archive on both success
	// and buffering failure, after BlobToSpec has closed its readers.
	defer func() {
		if closer, ok := downloadedBlob.(io.Closer); ok {
			if closeErr := closer.Close(); closeErr != nil {
				slog.WarnContext(ctx, "failed to close temporary git download", "err", closeErr)
			}
		}
	}()

	fileSpec, err := filesystem.BlobToSpec(downloadedBlob, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering git resource archive to file: %w", err)
	}
	transformation.Output = &v1alpha1.GetGitResourceOutput{
		File:     *fileSpec,
		Resource: transformation.Spec.Resource,
	}
	return &transformation, nil
}

func (t *GetGitResource) resolveCredentials(ctx context.Context, targetResource *descriptor.Resource) (runtime.Typed, error) {
	if t.CredentialProvider == nil {
		return nil, nil
	}
	consumerID, err := t.ResourceRepository.GetResourceCredentialConsumerIdentity(ctx, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed getting resource consumer identity for credential resolution: %w", err)
	}
	if consumerID == nil {
		return nil, nil
	}
	typed, err := t.CredentialProvider.Resolve(ctx, consumerID)
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed resolving git credentials: %w", err)
	}
	return typed, nil
}
