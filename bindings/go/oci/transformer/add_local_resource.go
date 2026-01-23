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

// AddLocalResource is a transformer that adds local resource blobs to component versions
// in OCI and CTF repositories. It downloads the blob from the resource's access spec
// and uploads it as a local resource to the target repository.
type AddLocalResource struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *AddLocalResource) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating add local resource transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to add local resource transformation: %w", err)
	}

	var repoSpec runtime.Typed
	var contentSpec blobv1alpha1.File
	var component, version string
	var resource *v2.Resource
	var output any

	switch tr := transformation.(type) {
	case *v1alpha1.OCIAddLocalResource:
		repoSpec = &tr.Spec.Repository
		component = tr.Spec.Component
		version = tr.Spec.Version
		resource = tr.Spec.Resource
		contentSpec = tr.Spec.File
		if tr.Output == nil {
			tr.Output = &v1alpha1.OCIAddLocalResourceOutput{}
		}
		output = tr.Output
	case *v1alpha1.CTFAddLocalResource:
		repoSpec = &tr.Spec.Repository
		component = tr.Spec.Component
		version = tr.Spec.Version
		resource = tr.Spec.Resource
		contentSpec = tr.Spec.File
		if tr.Output == nil {
			tr.Output = &v1alpha1.CTFAddLocalResourceOutput{}
		}
		output = tr.Output
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
		return nil, fmt.Errorf("file URI is required to upload the resource")
	}

	// Resolve credentials if provider available
	var creds map[string]string
	if t.CredentialProvider != nil {
		if consumerId, err := t.RepoProvider.GetComponentVersionRepositoryCredentialConsumerIdentity(ctx, repoSpec); err == nil {
			if creds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil && errors.Is(err, credentials.ErrNotFound) {
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

	// Add local resource - this will update the access spec with LocalBlob
	updatedResource, err := repo.AddLocalResource(ctx, component, version, runtimeResource, content)
	if err != nil {
		return nil, fmt.Errorf("failed adding local resource %q to component %s:%s: %w",
			runtimeResource.Name, component, version, err)
	}

	v2UpdatedResource, err := descriptor.ConvertToV2Resource(oci.DefaultRepositoryScheme, updatedResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting updated resource to v2 format: %w", err)
	}

	// Populate output based on type
	switch out := output.(type) {
	case *v1alpha1.OCIAddLocalResourceOutput:
		out.Resource = v2UpdatedResource
	case *v1alpha1.CTFAddLocalResourceOutput:
		out.Resource = v2UpdatedResource
	default:
		return nil, fmt.Errorf("unexpected output type: %T", output)
	}

	return transformation, nil
}
