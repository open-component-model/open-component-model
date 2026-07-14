package transformation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// GetWget is a transformer that downloads wget resources from their URL and buffers them to a file.
// The downloaded content is a plain blob; a subsequent AddLocalResource transformation is expected to
// embed it as a local blob in the target repository.
type GetWget struct {
	Scheme *runtime.Scheme
	// ResourceRepository is used to download wget resources and resolve credential consumer identities.
	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *GetWget) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.GetWget
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get wget transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for get wget transformation")
	}
	if transformation.Spec.Resource == nil {
		return nil, fmt.Errorf("resource is required in spec for get wget transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.GetWgetOutput{}
	}

	outputPath, err := determineOutputPath(transformation.Spec.OutputPath, "wget")
	if err != nil {
		return nil, fmt.Errorf("error getting output path: %w", err)
	}
	slog.DebugContext(ctx, "Going to use wget output path", "path", outputPath)

	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)

	creds, err := t.resolveCredentials(ctx, targetResource)
	if err != nil {
		return nil, err
	}

	slog.InfoContext(ctx, "Getting wget resource", "resource", transformation.Spec.Resource)

	downloadedBlob, err := t.ResourceRepository.DownloadResource(ctx, targetResource, creds)
	if err != nil {
		return nil, fmt.Errorf("error downloading wget resource: %w", err)
	}

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
func (t *GetWget) resolveCredentials(ctx context.Context, targetResource *descriptor.Resource) (runtime.Typed, error) {
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
