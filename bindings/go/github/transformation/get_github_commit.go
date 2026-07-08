// Package transformation provides the transformers for the gitHub access
// type, starting with GetGitHubCommit which buffers a repository tree at a
// pinned commit to a local tar archive.
package transformation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/github/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetGitHubCommit is a transformer that retrieves the tree of a GitHub
// repository at the commit pinned in the resource's gitHub access and buffers
// it to a local tar archive file.
type GetGitHubCommit struct {
	Scheme *runtime.Scheme
	// ResourceRepository is used to download github resources and resolve
	// credential consumer identities.
	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *GetGitHubCommit) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.GetGitHubCommit
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get github commit transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for get github commit transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.GetGitHubCommitOutput{}
	}

	contentOutputPath, err := DetermineOutputPath(transformation.Spec.OutputPath, "github-commit")
	if err != nil {
		return nil, fmt.Errorf("error getting content output path: %w", err)
	}
	slog.DebugContext(ctx, "Going to use content output path", "path", contentOutputPath)

	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)

	creds, err := t.resolveCredentials(ctx, targetResource)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Getting github commit", "resource", transformation.Spec.Resource)

	downloadedBlob, err := t.ResourceRepository.DownloadResource(ctx, targetResource, creds)
	if err != nil {
		return nil, fmt.Errorf("error downloading github commit: %w", err)
	}

	contentFileSpec, err := filesystem.BlobToSpec(downloadedBlob, contentOutputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering github commit archive to file: %w", err)
	}
	slog.DebugContext(ctx, "Converted github commit blob to file spec", "uri", contentFileSpec.URI)

	transformation.Output.ContentFile = *contentFileSpec
	transformation.Output.Resource = transformation.Spec.Resource

	return &transformation, nil
}

// resolveCredentials returns credentials for downloading targetResource, or
// nil if no credential provider is configured or the resource has no consumer
// identity. An ErrNotFound from the resolver is treated as "no credentials"
// rather than an error.
func (t *GetGitHubCommit) resolveCredentials(ctx context.Context, targetResource *descriptor.Resource) (runtime.Typed, error) {
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
	if err != nil {
		if errors.Is(err, credentials.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return typed, nil
}
