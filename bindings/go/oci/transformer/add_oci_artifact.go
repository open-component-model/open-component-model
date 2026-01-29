package transformer

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	blobv1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// AddOCIArtifact is a transformer that uploads OCI artifacts to target registries
// and updates the resource's access specification with the new image reference.
// For CTF repositories, it converts the artifact to a local blob instead.
type AddOCIArtifact struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *AddOCIArtifact) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating add oci artifact transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to add oci artifact transformation: %w", err)
	}

	var repoSpec runtime.Typed
	var contentSpec blobv1alpha1.File
	var component, version string
	var resource *v2.Resource
	var targetReference string
	var output any
	var isCTF bool

	switch tr := transformation.(type) {
	case *v1alpha1.OCIAddOCIArtifact:
		repoSpec = &tr.Spec.Repository
		component = tr.Spec.Component
		version = tr.Spec.Version
		resource = tr.Spec.Resource
		contentSpec = tr.Spec.File
		targetReference = tr.Spec.TargetReference
		if tr.Output == nil {
			tr.Output = &v1alpha1.OCIAddOCIArtifactOutput{}
		}
		output = tr.Output
		isCTF = false
	case *v1alpha1.CTFAddOCIArtifact:
		repoSpec = &tr.Spec.Repository
		component = tr.Spec.Component
		version = tr.Spec.Version
		resource = tr.Spec.Resource
		contentSpec = tr.Spec.File
		if tr.Output == nil {
			tr.Output = &v1alpha1.CTFAddOCIArtifactOutput{}
		}
		output = tr.Output
		isCTF = true
		// CTF doesn't use targetReference - converts to local blob
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}

	// Validate inputs
	if component == "" {
		return nil, fmt.Errorf("component name is required")
	}
	if version == "" {
		return nil, fmt.Errorf("component version is required")
	}
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	if contentSpec.URI == "" {
		return nil, fmt.Errorf("file URI is required to access the artifact data to be uploaded")
	}
	if !isCTF && targetReference == "" {
		return nil, fmt.Errorf("targetReference is required for OCI repository uploads")
	}

	// Resolve credentials if provider available
	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil && !errors.Is(err, credentials.ErrNotFound) {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	// Get repository
	repo, err := t.RepoProvider.GetComponentVersionRepository(ctx, repoSpec, creds)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version repository: %w", err)
	}

	// Convert v2.Resource to runtime.Resource
	runtimeResource := descriptor.ConvertFromV2Resource(resource)
	if runtimeResource == nil {
		return nil, fmt.Errorf("failed converting resource from v2 format")
	}

	// Get blob from file spec
	content, err := filesystem.GetBlobFromSpec(ctx, &contentSpec)
	if err != nil {
		return nil, fmt.Errorf("failed getting blob from file spec: %w", err)
	}

	var updatedResource *descriptor.Resource

	if isCTF {
		// CTF: Convert OCI artifact to local blob
		updatedResource, err = repo.AddLocalResource(ctx, component, version, runtimeResource, content)
		if err != nil {
			return nil, fmt.Errorf("failed adding OCI artifact as local resource to CTF %s:%s: %w",
				component, version, err)
		}
	} else {
		// OCI: Upload as external OCI artifact
		resourceRepo, ok := repo.(repository.ResourceRepository)
		if !ok {
			return nil, fmt.Errorf("repository does not support resource upload operations")
		}

		// Create a copy of the resource with updated access pointing to target
		uploadResource := runtimeResource.DeepCopy()
		
		// Update the access specification with the target reference
		// The UploadResource method will handle the actual upload
		if uploadResource.Access == nil {
			uploadResource.Access = &runtime.Raw{}
		}
		
		// Cast to *runtime.Raw to access Type and Data fields
		accessRaw, ok := uploadResource.Access.(*runtime.Raw)
		if !ok {
			return nil, fmt.Errorf("access is not a *runtime.Raw")
		}
		
		// Set the access type to ociArtifact with new imageReference
		accessData := fmt.Sprintf(`{"type":"ociArtifact","imageReference":"%s"}`, targetReference)
		accessRaw.Type = runtime.Type{Name: "ociArtifact"}
		accessRaw.Data = []byte(accessData)

		updatedResource, err = resourceRepo.UploadResource(ctx, uploadResource, content)
		if err != nil {
			return nil, fmt.Errorf("failed uploading OCI artifact %q to %s: %w",
				runtimeResource.Name, targetReference, err)
		}
	}

	// Convert updated resource back to v2 format
	var v2UpdatedResource *v2.Resource
	if isCTF {
		v2UpdatedResource, err = descriptor.ConvertToV2Resource(t.Scheme, updatedResource)
	} else {
		v2UpdatedResource, err = descriptor.ConvertToV2Resource(oci.DefaultRepositoryScheme, updatedResource)
	}
	if err != nil {
		return nil, fmt.Errorf("failed converting updated resource to v2 format: %w", err)
	}

	// Populate output based on type
	switch out := output.(type) {
	case *v1alpha1.OCIAddOCIArtifactOutput:
		out.Resource = v2UpdatedResource
	case *v1alpha1.CTFAddOCIArtifactOutput:
		out.Resource = v2UpdatedResource
	default:
		return nil, fmt.Errorf("unexpected output type: %T", output)
	}

	return transformation, nil
}
