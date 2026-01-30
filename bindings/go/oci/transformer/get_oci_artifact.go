package transformer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetOCIArtifact is a transformer that retrieves OCI artifacts from remote registries
// (referenced in component versions) and buffers them to files.
type GetOCIArtifact struct {
	Scheme             *runtime.Scheme
	RepoProvider       repository.ComponentVersionRepositoryProvider
	CredentialProvider credentials.Resolver
}

func (t *GetOCIArtifact) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating get oci artifact transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get oci artifact transformation: %w", err)
	}

	var repoSpec runtime.Typed
	var component, version string
	var resourceIdentity runtime.Identity
	var outputPath string
	var output interface{}

	switch tr := transformation.(type) {
	case *v1alpha1.OCIGetOCIArtifact:
		repoSpec = &tr.Spec.Repository
		component = tr.Spec.Component
		version = tr.Spec.Version
		resourceIdentity = tr.Spec.ResourceIdentity
		outputPath = tr.Spec.OutputPath
		if tr.Output == nil {
			tr.Output = &v1alpha1.OCIGetOCIArtifactOutput{}
		}
		output = tr.Output
	case *v1alpha1.CTFGetOCIArtifact:
		repoSpec = &tr.Spec.Repository
		component = tr.Spec.Component
		version = tr.Spec.Version
		resourceIdentity = tr.Spec.ResourceIdentity
		outputPath = tr.Spec.OutputPath
		if tr.Output == nil {
			tr.Output = &v1alpha1.CTFGetOCIArtifactOutput{}
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

	// Get component descriptor to find the resource
	desc, err := repo.GetComponentVersion(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %w", component, version, err)
	}

	// Find the resource in the descriptor
	var targetResource *descriptor.Resource
	for _, res := range desc.Component.Resources {
		resIdentity := res.ToIdentity()
		// Compare identities by checking if all keys match
		matches := true
		if len(resIdentity) != len(resourceIdentity) {
			matches = false
		} else {
			for k, v := range resourceIdentity {
				if resVal, ok := resIdentity[k]; !ok || resVal != v {
					matches = false
					break
				}
			}
		}
		if matches {
			targetResource = &res
			break
		}
	}
	if targetResource == nil {
		return nil, fmt.Errorf("resource with identity %v not found in component %s:%s", resourceIdentity, component, version)
	}

	// Download the resource using DownloadResource (for external OCI artifacts)
	// This requires the repository to support ResourceRepository interface
	resourceRepo, ok := repo.(repository.ResourceRepository)
	if !ok {
		return nil, fmt.Errorf("repository does not support resource download operations")
	}

	blobContent, err := resourceRepo.DownloadResource(ctx, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed downloading OCI artifact %v from component %s:%s: %w",
			resourceIdentity, component, version, err)
	}

	// Determine output path
	if outputPath == "" {
		// Create a temporary file
		tempFile, err := os.CreateTemp("", "oci-artifact-*.tar")
		if err != nil {
			return nil, fmt.Errorf("failed creating temporary file: %w", err)
		}
		tempFile.Close() // Close immediately, BlobToSpec will overwrite it
		outputPath = tempFile.Name()
	} else {
		// Ensure the directory exists
		dir := filepath.Dir(outputPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("failed creating output directory: %w", err)
		}
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

	// Populate output based on type
	switch out := output.(type) {
	case *v1alpha1.OCIGetOCIArtifactOutput:
		out.File = *fileSpec
		out.Resource = v2Resource
	case *v1alpha1.CTFGetOCIArtifactOutput:
		out.File = *fileSpec
		out.Resource = v2Resource
	default:
		return nil, fmt.Errorf("unexpected output type: %T", output)
	}

	return transformation, nil
}
