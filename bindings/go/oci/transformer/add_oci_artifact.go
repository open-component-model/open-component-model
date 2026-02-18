package transformer

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// AddOCIArtifact is a transformer that uploads OCI artifacts to remote registries.
type AddOCIArtifact struct {
	Scheme             *runtime.Scheme
	Repository         repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *AddOCIArtifact) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.AddOCIArtifact
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to add oci artifact transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for add oci artifact transformation")
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.AddOCIArtifactOutput{}
	}

	// Validate inputs
	if transformation.Spec.Resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if transformation.Spec.File.URI == "" {
		return nil, fmt.Errorf("file is required")
	}

	// Convert resource to internal format
	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)

	// Resolve credentials if credential provider is available
	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.Repository.GetResourceCredentialConsumerIdentity(ctx, targetResource); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	// Get blob from file spec
	blobContent, err := filesystem.GetBlobFromSpec(ctx, &transformation.Spec.File)
	if err != nil {
		return nil, fmt.Errorf("failed reading blob from file %s: %w", transformation.Spec.File.URI, err)
	}

	// Upload blob to repository - this will update the access spec with the oci access
	updatedResource, err := t.Repository.UploadResource(ctx, targetResource, blobContent, creds)
	if err != nil {
		return nil, fmt.Errorf("failed uploading OCI artifact %v: %w", targetResource.ToIdentity(), err)
	}

	// Convert resource back to v2 format
	v2UpdatedResource, err := descriptor.ConvertToV2Resource(t.Scheme, updatedResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	// Populate output
	transformation.Output.Resource = v2UpdatedResource

	return &transformation, nil
}
