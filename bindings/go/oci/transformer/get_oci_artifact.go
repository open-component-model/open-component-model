package transformer

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetOCIArtifact is a transformer that retrieves OCI artifacts from remote registries
// and buffers them to files.
type GetOCIArtifact struct {
	Scheme             *runtime.Scheme
	Repository         repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *GetOCIArtifact) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.GetOCIArtifact
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get oci artifact transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for get oci artifact transformation")
	}

	var resource *v2.Resource
	var outputPath string
	var output *v1alpha1.GetOCIArtifactOutput

	resource = transformation.Spec.Resource
	outputPath = transformation.Spec.OutputPath
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.GetOCIArtifactOutput{}
	}
	output = transformation.Output

	// Validate inputs
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	targetResource := descriptor.ConvertFromV2Resource(resource)

	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.Repository.GetResourceCredentialConsumerIdentity(ctx, targetResource); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	blobContent, err := t.Repository.DownloadResource(ctx, targetResource, creds)
	if err != nil {
		return nil, fmt.Errorf("failed downloading OCI artifact %v %w", resource.ToIdentity(), err)
	}

	// Determine output path
	if outputPath, err = DetermineOutputPath(outputPath, "oci-artifact", blobContent); err != nil {
		return nil, fmt.Errorf("failed determining output path: %w", err)
	}

	// Buffer blob to file
	fileSpec, err := filesystem.BlobToSpec(blobContent, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering blob to file: %w", err)
	}

	// Convert resource to v2 format
	v2Resource, err := descriptor.ConvertToV2Resource(t.Scheme, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	// Populate output
	output.File = *fileSpec
	output.Resource = v2Resource

	return &transformation, nil
}
