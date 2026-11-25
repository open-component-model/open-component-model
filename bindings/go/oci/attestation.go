package oci

import (
	"context"
	"fmt"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	slogcontext "github.com/veqryn/slog-context"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/internal/attestation"
	"ocm.software/open-component-model/bindings/go/oci/internal/introspection"
	"ocm.software/open-component-model/bindings/go/oci/internal/referrer"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	ocmrepository "ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	_ ocmrepository.LocalAttestationDiscoverer     = (*Repository)(nil)
	_ ocmrepository.ComponentAttestationDiscoverer = (*Repository)(nil)
)

// DiscoverAttestations returns, metadata-only, every attestation attached to the OCI
// artifact backing res: the OCI referrers plus any BuildKit in-toto attestations in its
// image index. Nothing is filtered or interpreted. Credentials are already held by the
// resolver.
func (repo *Repository) DiscoverAttestations(ctx context.Context, res *descriptor.Resource) ([]ocmrepository.Attestation, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)

	if res.Access == nil || res.Access.GetType().IsEmpty() {
		return nil, fmt.Errorf("resource access type is empty")
	}

	reference, err := repo.ociImageReference(res.Access)
	if err != nil {
		return nil, err
	}

	store, root, err := repo.resolveRoot(ctx, reference)
	if err != nil {
		return nil, err
	}
	return discoverFromRoot(ctx, store, root)
}

// DiscoverLocalAttestations discovers the attestations of a resource whose artifact was
// copied into the component as a local blob (a by-value transfer, for example into a
// CTF), entirely within the local store. Mirrors DiscoverLocalSBOM.
func (repo *Repository) DiscoverLocalAttestations(ctx context.Context, component, version string, identity runtime.Identity) ([]ocmrepository.Attestation, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)

	cv, err := repo.readComponentVersion(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed to get component version: %w", err)
	}
	store := cv.store

	res, err := localResourceFromDescriptor(cv.descriptor, identity)
	if err != nil {
		return nil, err
	}
	local, err := repo.localBlobAccess(res)
	if err != nil {
		return nil, err
	}
	// A local blob that is not an OCI image manifest or index (for example an SBOM or
	// VEX document stored by value) carries neither referrers nor a BuildKit index.
	// Resolving its digest as a manifest only triggers a slow fallback lookup, so stop
	// here before touching the store.
	if !introspection.IsOCICompliantMediaType(local.MediaType) {
		return nil, nil
	}

	localRef, err := withDigest(cv.reference, local.LocalReference)
	if err != nil {
		return nil, err
	}
	root, err := store.Resolve(ctx, localRef)
	if err != nil {
		return nil, fmt.Errorf("resolving local blob %q of resource %q failed: %w", local.LocalReference, identity, err)
	}
	root.MediaType = local.MediaType

	return discoverFromRoot(ctx, store, root)
}

// DiscoverComponentAttestations returns the referrers attached to the component version
// artifact itself (the subject the "component-descriptors/<component>:<version>" tag
// resolves to). Component level is referrer-only: a component has neither a BuildKit
// index nor per-resource labels.
func (repo *Repository) DiscoverComponentAttestations(ctx context.Context, component, version string) ([]ocmrepository.Attestation, error) {
	ctx = slogcontext.NewCtx(ctx, repo.logger)

	ref, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return nil, err
	}
	_, root, err := repo.resolveRootIn(ctx, store, ref)
	if err != nil {
		return nil, err
	}
	return referrer.DiscoverAttestations(ctx, store, root)
}

// resolveRoot resolves reference to its store and artifact root descriptor.
func (repo *Repository) resolveRoot(ctx context.Context, reference string) (spec.Store, ociImageSpecV1.Descriptor, error) {
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("getting store for reference %q failed: %w", reference, err)
	}
	return repo.resolveRootIn(ctx, store, reference)
}

func (repo *Repository) resolveRootIn(ctx context.Context, store spec.Store, reference string) (spec.Store, ociImageSpecV1.Descriptor, error) {
	parsed, err := looseref.ParseReference(reference)
	if err != nil {
		return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("parsing reference %q failed: %w", reference, err)
	}
	root, err := store.Resolve(ctx, parsed.GetReference())
	if err != nil {
		return nil, ociImageSpecV1.Descriptor{}, fmt.Errorf("resolving reference %q failed: %w", reference, err)
	}
	return store, root, nil
}

// discoverFromRoot merges the two artifact-level sources: OCI referrers and the BuildKit
// in-toto index. Both only exist on an OCI image manifest or index. A plain blob stored
// by value (for example an SBOM document) can carry neither, and probing its referrers
// triggers a slow fallback-tag lookup that always returns empty, so skip it entirely.
func discoverFromRoot(ctx context.Context, store spec.Store, root ociImageSpecV1.Descriptor) ([]ocmrepository.Attestation, error) {
	if !introspection.IsOCICompliantManifest(root) {
		return nil, nil
	}
	out, err := referrer.DiscoverAttestations(ctx, store, root)
	if err != nil {
		return nil, err
	}
	fromIndex, err := attestation.DiscoverAllFromRoot(ctx, store, root)
	if err != nil {
		return nil, err
	}
	return append(out, fromIndex...), nil
}
