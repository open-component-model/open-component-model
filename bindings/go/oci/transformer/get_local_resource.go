package transformer

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetLocalResource is a transformer that retrieves local resource blobs from component versions
// in OCI and CTF repositories and buffers them to files.
type GetLocalResource struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *GetLocalResource) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating get local resource transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get local resource transformation: %w", err)
	}

	var repoSpec runtime.Typed
	var component, version string
	var resourceIdentity runtime.Identity
	var outputPath string
	var output interface{}

	switch tr := transformation.(type) {
	case *v1alpha1.OCIGetLocalResource:
		repoSpec = &tr.Spec.Repository
		component = tr.Spec.Component
		version = tr.Spec.Version
		resourceIdentity = tr.Spec.ResourceIdentity
		outputPath = tr.Spec.OutputPath
		if tr.Output == nil {
			tr.Output = &v1alpha1.OCIGetLocalResourceOutput{}
		}
		output = tr.Output
	case *v1alpha1.CTFGetLocalResource:
		repoSpec = &tr.Spec.Repository
		component = tr.Spec.Component
		version = tr.Spec.Version
		resourceIdentity = tr.Spec.ResourceIdentity
		outputPath = tr.Spec.OutputPath
		if tr.Output == nil {
			tr.Output = &v1alpha1.CTFGetLocalResourceOutput{}
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
	if len(resourceIdentity) == 0 {
		return nil, fmt.Errorf("resource identity is required")
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

	// Get local resource
	blobContent, runtimeResource, err := repo.GetLocalResource(ctx, component, version, resourceIdentity)
	if err != nil {
		return nil, fmt.Errorf("failed getting local resource %v from component %s:%s: %w",
			resourceIdentity, component, version, err)
	}

	// Determine output path
	if outputPath, err = DetermineOutputPath(outputPath, "resource"); err != nil {
		return nil, fmt.Errorf("failed determining output path: %w", err)
	}

	// Buffer blob to file
	fileSpec, err := filesystem.BlobToSpec(blobContent, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering blob to file: %w", err)
	}

	// Convert resource to v2 format
	v2Resource, err := descriptor.ConvertToV2Resource(t.Scheme, runtimeResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	// Populate output based on type
	switch out := output.(type) {
	case *v1alpha1.OCIGetLocalResourceOutput:
		out.File = *fileSpec
		out.Resource = v2Resource
	case *v1alpha1.CTFGetLocalResourceOutput:
		out.File = *fileSpec
		out.Resource = v2Resource
	default:
		return nil, fmt.Errorf("unexpected output type: %T", output)
	}

	return transformation, nil
}
