package transformer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetOCIArtifact is a transformer that retrieves OCI artifacts from remote registries
// and buffers them to files.
type GetOCIArtifact struct {
	Scheme     *runtime.Scheme
	Repository oci.ResourceRepository
}

func (t *GetOCIArtifact) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	transformation, err := t.Scheme.NewObject(step.GetType())
	if err != nil {
		return nil, fmt.Errorf("failed creating get oci artifact transformation object: %w", err)
	}
	if err := t.Scheme.Convert(step, transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to get oci artifact transformation: %w", err)
	}

	var resource *v2.Resource
	var outputPath string
	var output interface{}

	switch tr := transformation.(type) {
	case *v1alpha1.OCIGetOCIArtifact:
		resource = tr.Spec.Resource
		outputPath = tr.Spec.OutputPath
		if tr.Output == nil {
			tr.Output = &v1alpha1.OCIGetOCIArtifactOutput{}
		}
		output = tr.Output
	default:
		return nil, fmt.Errorf("unexpected transformation type: %T", transformation)
	}

	// Validate inputs
	if resource == nil {
		return nil, fmt.Errorf("resource is required")
	}
	targetResource := descriptor.ConvertFromV2Resource(resource)

	blobContent, err := t.Repository.DownloadResource(ctx, targetResource)
	if err != nil {
		return nil, fmt.Errorf("failed downloading OCI artifact %v %w", resource, err)
	}

	// Determine output path
	if outputPath == "" {
		// Create a temporary file
		tempFile, err := os.CreateTemp("", "oci-artifact-*.tar")
		if err != nil {
			return nil, fmt.Errorf("failed creating temporary file: %w", err)
		}
		_ = tempFile.Close() // Close immediately, BlobToSpec will overwrite it
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
	default:
		return nil, fmt.Errorf("unexpected output type: %T", output)
	}

	return transformation, nil
}
