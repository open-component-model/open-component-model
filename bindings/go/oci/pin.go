package oci

import (
	"context"
	"errors"
	"fmt"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/errdef"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	"ocm.software/open-component-model/bindings/go/repository"
)

// componentVersionPin pins the reads of a component to the artifact (OCI image index or manifest)
// with digest, see [Repository.PinComponentVersion].
type componentVersionPin struct {
	component string
	// version is the version the artifact must hold; empty if any version is accepted.
	version string
	digest  digest.Digest
}

// storedComponentVersion is a component version as read from its store.
type storedComponentVersion struct {
	// reference is the reference the component version was resolved by: a tag reference,
	// or a digest reference if the repository is pinned.
	reference  string
	store      spec.Store
	descriptor *descriptor.Descriptor
	manifest   *ociImageSpecV1.Manifest
	// index is the index the manifest is part of, nil if the component version is stored as manifest.
	index *ociImageSpecV1.Index
}

// PinComponentVersion implements [repository.DigestPinnableRepository].
//
// The digest is the one of the OCI image index or manifest a component version is stored as, which
// is what its version tag resolves to. Each node read from it is verified against its digest, so the
// pinned digest covers the descriptor and every local blob of the component version.
func (repo *Repository) PinComponentVersion(component, version, dgst string) (repository.ComponentVersionRepository, error) {
	d, err := digest.Parse(dgst)
	if err != nil {
		return nil, fmt.Errorf("invalid OCI digest %q: %w", dgst, err)
	}
	pinned := *repo
	pinned.pin = &componentVersionPin{component: component, version: version, digest: d}
	return &pinned, nil
}

// ResolveComponentVersionDigest implements [repository.DigestPinnableRepository].
func (repo *Repository) ResolveComponentVersionDigest(ctx context.Context, component, version string) (string, error) {
	reference, store, err := repo.getStore(ctx, component, version)
	if err != nil {
		return "", err
	}
	desc, err := store.Resolve(ctx, reference)
	if err != nil {
		return "", fmt.Errorf("resolving component version %s:%s failed: %w", component, version, err)
	}
	return desc.Digest.String(), nil
}

// readComponentVersion reads the descriptor of component in version together with the OCI
// artifact it is stored as. If the repository is pinned to an artifact of component, the
// descriptor is read from that artifact instead of resolving the version tag.
func (repo *Repository) readComponentVersion(ctx context.Context, component, version string) (*storedComponentVersion, error) {
	pin := repo.pin
	if pin == nil || pin.component != component {
		reference, store, err := repo.getStore(ctx, component, version)
		if err != nil {
			return nil, err
		}
		desc, manifest, index, err := getDescriptorFromStore(ctx, store, reference, repo.unmarshalDescriptorFunc)
		if err != nil {
			return nil, err
		}
		return &storedComponentVersion{reference: reference, store: store, descriptor: desc, manifest: manifest, index: index}, nil
	}

	reference, store, err := repo.getPinnedStore(ctx, pin)
	if err != nil {
		return nil, err
	}
	desc, manifest, index, err := getDescriptorFromStore(ctx, store, reference, repo.unmarshalDescriptorFunc)
	if err != nil {
		if errors.Is(err, errdef.ErrNotFound) {
			return nil, repo.pinNotFoundError(ctx, pin, version, err)
		}
		return nil, err
	}
	// The descriptor is part of the pinned artifact, so it authentically names what the artifact holds.
	for _, want := range []string{pin.version, version} {
		if desc.Component.Name != component || want != "" && desc.Component.Version != want {
			return nil, fmt.Errorf("artifact %s holds component version %s:%s, not %s%s",
				pin.digest, desc.Component.Name, desc.Component.Version, component, versionSuffix(want))
		}
	}
	return &storedComponentVersion{reference: reference, store: store, descriptor: desc, manifest: manifest, index: index}, nil
}

// getPinnedStore returns the store of the pinned component and the reference of the pinned artifact in it.
func (repo *Repository) getPinnedStore(ctx context.Context, pin *componentVersionPin) (string, spec.Store, error) {
	// Resolvers only build tag references. Any tag names the repository of the component
	// (see ListComponentVersions), which is then addressed by digest.
	reference, err := withDigest(repo.resolver.ComponentVersionReference(ctx, pin.component, "latest"), pin.digest.String())
	if err != nil {
		return "", nil, err
	}
	store, err := repo.resolver.StoreForReference(ctx, reference)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get store for reference: %w", err)
	}
	return reference, store, nil
}

// pinNotFoundError reports that no artifact with the pinned digest exists. If the version is
// known, it also reports what the version tag currently resolves to.
func (repo *Repository) pinNotFoundError(ctx context.Context, pin *componentVersionPin, version string, cause error) error {
	if version == "" {
		version = pin.version
	}
	if version != "" {
		if reference, store, err := repo.getStore(ctx, pin.component, version); err == nil {
			if current, err := store.Resolve(ctx, reference); err == nil {
				return fmt.Errorf("component version %s:%s does not match pinned digest %s: resolved digest is %s: %w",
					pin.component, version, pin.digest, current.Digest, cause)
			}
		}
	}
	return fmt.Errorf("component %s has no artifact with pinned digest %s: %w", pin.component, pin.digest, cause)
}

// checkPinnedTag fails if the repository is pinned to an artifact of component and the version
// tag (reference in store) resolves to another artifact: writing would overwrite a tag that moved
// away from the pin. A tag that does not exist yet is not overwritten and passes.
// OCI distribution has no conditional tag update, so a tag moved between this check and the
// write is still overwritten.
func (repo *Repository) checkPinnedTag(ctx context.Context, store spec.Store, reference, component, version string) error {
	pin := repo.pin
	if pin == nil || pin.component != component {
		return nil
	}
	current, err := store.Resolve(ctx, reference)
	if errors.Is(err, errdef.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("resolving version tag of %s:%s pinned to %s failed: %w", component, version, pin.digest, err)
	}
	if current.Digest != pin.digest {
		return fmt.Errorf("version tag of %s:%s resolves to %s, not to the pinned %s: "+
			"refusing to overwrite the moved tag with the pinned component version", component, version, current.Digest, pin.digest)
	}
	return nil
}

// withDigest returns reference with its tag or digest replaced by dgst.
func withDigest(reference, dgst string) (string, error) {
	ref, err := looseref.ParseReference(reference)
	if err != nil {
		return "", fmt.Errorf("invalid reference %q: %w", reference, err)
	}
	ref.Tag = ""
	ref.Reference.Reference = dgst
	return ref.String(), nil
}

func versionSuffix(version string) string {
	if version == "" {
		return ""
	}
	return ":" + version
}
