package transformer

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// AddOCIArtifact is a transformer that uploads an OCI artifact blob to a target registry.
// The blob is provided as input (typically from a previous GetOCIArtifact transformation).
// This transformer does not download the artifact - that should be done in a separate step.
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

	spec := transformation.Spec
	if spec.OCIArtifact == nil {
		return nil, fmt.Errorf("ociArtifact blob is required")
	}
	if spec.TargetRegistry == "" {
		return nil, fmt.Errorf("targetRef is required")
	}

	if transformation.Output == nil {
		transformation.Output = &v1alpha1.AddOCIArtifactOutput{}
	}
	output := transformation.Output

	// Create a resource descriptor with OCIImage access pointing to the target
	resource := &descriptor.Resource{
		Access: &accessv1.OCIImage{
			ImageReference: spec.TargetRegistry,
		},
	}

	// Resolve credentials for the target registry
	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.Repository.GetResourceCredentialConsumerIdentity(ctx, resource); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	// Upload the OCI artifact blob to the target registry
	updatedResource, err := t.Repository.UploadResource(ctx, resource, spec.OCIArtifact, creds)
	if err != nil {
		return nil, fmt.Errorf("failed uploading OCI artifact to %q: %w", spec.TargetRegistry, err)
	}

	// Convert updated resource to v2 format
	v2Resource, err := descriptor.ConvertToV2Resource(oci.DefaultRepositoryScheme, updatedResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	// Populate output
	output.Resource = v2Resource

	return &transformation, nil
}
