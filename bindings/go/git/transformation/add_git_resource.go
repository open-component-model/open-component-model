package transformation

import (
	"context"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/git/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// AddGitResource pushes a Git archive buffered by GetGitResource, or a local blob holding
// one, into the repository and ref of the resource's Git access, with credentials
// resolved for that target.
type AddGitResource struct {
	Scheme *runtime.Scheme
	// ResourceRepository uploads Git resources and resolves credential consumer identities.
	ResourceRepository repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *AddGitResource) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.AddGitResource
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to add git resource transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for add git resource transformation")
	}
	if transformation.Spec.Resource == nil {
		return nil, fmt.Errorf("resource is required in spec for add git resource transformation")
	}
	if transformation.Spec.File.URI == "" {
		return nil, fmt.Errorf("file is required in spec for add git resource transformation")
	}

	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)
	creds, err := resolveCredentials(ctx, t.ResourceRepository, t.CredentialProvider, targetResource)
	if err != nil {
		return nil, err
	}

	archive, err := filesystem.GetBlobFromSpec(ctx, &transformation.Spec.File)
	if err != nil {
		return nil, fmt.Errorf("failed reading git archive from file %s: %w", transformation.Spec.File.URI, err)
	}
	uploaded, err := t.ResourceRepository.UploadResource(ctx, targetResource, archive, creds)
	if err != nil {
		return nil, fmt.Errorf("failed uploading git resource %v: %w", targetResource.ToIdentity(), err)
	}
	v2Uploaded, err := descriptor.ConvertToV2Resource(t.Scheme, uploaded)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	transformation.Output = &v1alpha1.AddGitResourceOutput{Resource: v2Uploaded}
	return &transformation, nil
}
