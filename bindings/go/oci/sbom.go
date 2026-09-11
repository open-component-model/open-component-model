package oci

import (
	"context"
	"fmt"

	slogcontext "github.com/veqryn/slog-context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/internal/attestation"
	ociidentity "ocm.software/open-component-model/bindings/go/oci/internal/identity"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	accessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocmrepository "ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var _ ocmrepository.LocalSBOMDiscoverer = (*Repository)(nil)

// DiscoverSBOM returns the SBOM attestations attached to the OCI artifact.
func (repo *Repository) DiscoverSBOM(ctx context.Context, res *descriptor.Resource, opts ...ocmrepository.SBOMOption) ([]ocmrepository.SBOM, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)

	if res.Access == nil || res.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("resource access type is empty")
	}

	// user set value should overwrite the platform request.
	if platform := ociidentity.PlatformFromIdentity(res.ToIdentity()); platform != nil {
		opts = append([]ocmrepository.SBOMOption{ocmrepository.WithSBOMPlatform(ociidentity.ToRepositoryPlatform(*platform))}, opts...)
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

// DiscoverLocalSBOM will discover SBOMs that are attached to localblobs of type OCI.
func (repo *Repository) DiscoverLocalSBOM(ctx context.Context, component, version string, identity runtime.Identity, opts ...ocmrepository.SBOMOption) ([]ocmrepository.SBOM, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)

	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, err
	}

	res, err := repo.localResourceFromDescriptor(ctx, store, reference, identity)
	if err != nil {
		return nil, err
	}

	// user set value should overwrite the platform request.
	if platform := ociidentity.PlatformFromIdentity(res.ToIdentity()); platform != nil {
		opts = append([]ocmrepository.SBOMOption{ocmrepository.WithSBOMPlatform(ociidentity.ToRepositoryPlatform(*platform))}, opts...)
	}

	local, err := repo.localBlobAccess(res)
	if err != nil {
		return nil, err
	}

	if !attestation.IsIndex(local.MediaType) {
		return nil, fmt.Errorf("the local blob has media type %q and is not an image index: %w", local.MediaType, ocmrepository.ErrSBOMNotInspectable)
	}

	// LocalReference is always a digest, and this is OCI land, so we should be okay with doing this here.
	root, err := store.Resolve(ctx, reference+"@"+local.LocalReference)
	if err != nil {
		return nil, fmt.Errorf("resolving local blob %q of resource %q failed: %w", local.LocalReference, identity, err)
	}
	root.MediaType = local.MediaType

	return attestation.DiscoverSBOMsFromRoot(ctx, store, root, identity.String(), opts...)
}

// localResourceFromDescriptor finds the unique resource matching identity in the
// component version's descriptor.
func (repo *Repository) localResourceFromDescriptor(ctx context.Context, store spec.Store, reference string, identity runtime.Identity) (*descriptor.Resource, error) {
	desc, _, _, err := getDescriptorFromStore(ctx, store, reference, repo.unmarshalDescriptorFunc)
	if err != nil {
		return nil, fmt.Errorf("failed to get component version: %w", err)
	}

	artifacts := make([]descriptor.Artifact, 0, len(desc.Component.Resources))
	for i := range desc.Component.Resources {
		artifacts = append(artifacts, &desc.Component.Resources[i])
	}
	candidates := descriptor.FindArtifactsByIdentity(identity, artifacts)
	if len(candidates) != 1 {
		return nil, fmt.Errorf("found %d candidates while looking for resource %q, but expected exactly one", len(candidates), identity)
	}

	resource, ok := candidates[0].(*descriptor.Resource)
	if !ok {
		return nil, fmt.Errorf("candidate was not of type *descriptor.Request but was %T", candidates[0])
	}

	return resource, nil
}

// localBlobAccess converts a resource's access into a local blob, failing when it is
// not one.
func (repo *Repository) localBlobAccess(res *descriptor.Resource) (*v2.LocalBlob, error) {
	access := res.GetAccess()
	if access == nil || access.GetType().IsEmpty() {
		return nil, fmt.Errorf("resource access type is empty")
	}
	typed, err := repo.scheme.NewObject(access.GetType())
	if err != nil {
		return nil, fmt.Errorf("error creating resource access: %w", err)
	}
	if err := repo.scheme.Convert(access, typed); err != nil {
		return nil, fmt.Errorf("error converting resource access: %w", err)
	}
	local, ok := typed.(*v2.LocalBlob)
	if !ok {
		return nil, fmt.Errorf("resource access is %T, not a local blob", typed)
	}

	return local, nil
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
