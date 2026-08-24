package oci

import (
	"context"
	"fmt"

	slogcontext "github.com/veqryn/slog-context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/attestation"
	"ocm.software/open-component-model/bindings/go/oci/internal/identity"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// DiscoverSBOM returns the SBOM attestations attached to the OCI artifact.
func (repo *Repository) DiscoverSBOM(ctx context.Context, res *descriptor.Resource, opts ...repository.SBOMOption) ([]repository.SBOM, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)

	if res.Access == nil || res.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("resource access type is empty")
	}

	// user set value should overwrite the platform request.
	if platform := identity.PlatformFromIdentity(res.ToIdentity()); platform != nil {
		opts = append([]repository.SBOMOption{repository.WithSBOMPlatform(*platform)}, opts...)
	}

	reference, err := repo.ociImageReference(res.Access)
	if err != nil {
		return nil, err
	}

	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("getting store for reference %q failed: %w", reference, err)
	}

	return attestation.DiscoverSBOMs(ctx, store, reference, opts...)
}

// ociImageReference resolves an access specification down to the image reference it
// ultimately points at, following a local blob to its global access.
func (repo *Repository) ociImageReference(access runtime.Typed) (string, error) {
	typed, err := repo.scheme.NewObject(access.GetType())
	if err != nil {
		return "", fmt.Errorf("error creating resource access: %w", err)
	}
	if err := repo.scheme.Convert(access, typed); err != nil {
		return "", fmt.Errorf("error converting resource access: %w", err)
	}

	switch typed := typed.(type) {
	case *v2.LocalBlob:
		// look for the sbom at the original location.
		if typed.GlobalAccess == nil {
			return "", fmt.Errorf("local blob access does not have a global access and cannot be used")
		}
		globalAccess, err := repo.scheme.NewObject(typed.GlobalAccess.GetType())
		if err != nil {
			return "", fmt.Errorf("error creating typed global blob access with help of scheme: %w", err)
		}
		if err := repo.scheme.Convert(typed.GlobalAccess, globalAccess); err != nil {
			return "", fmt.Errorf("error converting global blob access: %w", err)
		}
		return repo.ociImageReference(globalAccess)
	case *accessv1.OCIImage:
		return typed.ImageReference, nil
	default:
		return "", fmt.Errorf("unsupported resource access type for SBOM discovery: %T", typed)
	}
}
