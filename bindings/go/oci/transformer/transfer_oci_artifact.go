package transformer

import (
	"context"
	"errors"
	"fmt"

	"ocm.software/open-component-model/bindings/go/credentials"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TransferOCIArtifact is a fused transformer that streams OCI artifacts directly
// between registries using ResourceStream, bypassing tar materialization.
type TransferOCIArtifact struct {
	Scheme             *runtime.Scheme
	Repository         repository.ResourceRepository
	CredentialProvider credentials.Resolver
}

func (t *TransferOCIArtifact) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.TransferOCIArtifact
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to transfer oci artifact transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for transfer oci artifact transformation")
	}
	if transformation.Spec.Resource == nil {
		return nil, fmt.Errorf("source resource is required")
	}
	if transformation.Spec.TargetResource == nil {
		return nil, fmt.Errorf("target resource is required")
	}
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.TransferOCIArtifactOutput{}
	}

	srcResource := descriptor.ConvertFromV2Resource(transformation.Spec.Resource)
	targetResource := descriptor.ConvertFromV2Resource(transformation.Spec.TargetResource)

	// Resolve source credentials
	var srcCreds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerId, err := t.Repository.GetResourceCredentialConsumerIdentity(ctx, srcResource); err == nil {
			if srcCreds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil {
				if !errors.Is(err, credentials.ErrNotFound) {
					return nil, fmt.Errorf("failed resolving source credentials: %w", err)
				}
			}
		}
	}

	// Resolve target credentials
	var dstCreds runtime.Typed
	if t.CredentialProvider != nil {
		if consumerId, err := t.Repository.GetResourceCredentialConsumerIdentity(ctx, targetResource); err == nil {
			if dstCreds, err = t.CredentialProvider.Resolve(ctx, consumerId); err != nil {
				if !errors.Is(err, credentials.ErrNotFound) {
					return nil, fmt.Errorf("failed resolving target credentials: %w", err)
				}
			}
		}
	}

	// Try streaming path
	streamingRepo, ok := t.Repository.(ocistream.ResourceRepository)
	if !ok {
		return nil, fmt.Errorf("repository does not support streaming transfers")
	}

	stream, err := streamingRepo.DownloadResourceStream(ctx, srcResource, srcCreds)
	if err != nil {
		return nil, fmt.Errorf("failed creating resource stream for %v: %w", srcResource.ToIdentity(), err)
	}

	updatedResource, err := streamingRepo.UploadResourceStream(ctx, targetResource, stream, dstCreds)
	if err != nil {
		return nil, fmt.Errorf("failed streaming OCI artifact %v: %w", srcResource.ToIdentity(), err)
	}

	// Convert resource back to v2 format
	v2UpdatedResource, err := descriptor.ConvertToV2Resource(t.Scheme, updatedResource)
	if err != nil {
		return nil, fmt.Errorf("failed converting resource to v2 format: %w", err)
	}

	transformation.Output.Resource = v2UpdatedResource
	return &transformation, nil
}
