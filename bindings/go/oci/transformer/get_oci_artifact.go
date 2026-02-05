package transformer

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var mediaTypExtMap = map[string]string{
	layout.MediaTypeOCIImageLayoutTarGzipV1: "tar.gz",
}

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
			creds, err = t.CredentialProvider.Resolve(ctx, consumerId)
			if err != nil {
				return nil, fmt.Errorf("failed resolving credentials: %w", err)
			}
		}
	}

	blobContent, err := t.Repository.DownloadResource(ctx, targetResource, creds)
	if err != nil {
		return nil, fmt.Errorf("failed downloading OCI artifact %v %w", resource.ToIdentity(), err)
	}

	// Determine output path
	if outputPath == "" {
		fileExt := ""
		if mediaTypeAware, ok := blobContent.(blob.MediaTypeAware); ok {
			if mediaType, ok := mediaTypeAware.MediaType(); ok {
				fileExt = mediaTypExtMap[mediaType]
			}
		}

		if fileExt == "" {
			slog.Warn("unable to determine file extension from media type, setting no extension")
		} else {
			fileExt = fmt.Sprintf(".%s", fileExt)
		}

		// Create a temporary file
		tempFile, err := os.CreateTemp("", fmt.Sprintf("oci-artifact-*%s", fileExt))
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

	// Populate output
	output.File = *fileSpec
	output.Resource = v2Resource

	return &transformation, nil
}
