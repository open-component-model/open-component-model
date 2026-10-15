package ocm

import (
	"context"
	"fmt"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/repository"
)

// DigestPin returns the digest the component reference is pinned to (see
// [repository.ParseDigestPin]), or nil if it carries no digest. An OCI digest addresses the
// component version by itself, so the version may be omitted. A normalized digest is verified
// against the descriptor read by version, so a reference pinned to it must name the version.
func DigestPin(ref *compref.Ref) (*descruntime.Digest, error) {
	if ref.Digest == "" {
		return nil, nil
	}
	pin, err := repository.ParseDigestPin(ref.Digest)
	if err != nil {
		return nil, fmt.Errorf("component reference %q: %w", ref.String(), err)
	}
	if pin.NormalisationAlgorithm != "" && ref.Version == "" {
		return nil, fmt.Errorf("component reference %q is pinned to the normalized digest %q but has no version: name the version", ref.String(), ref.Digest)
	}
	return pin, nil
}

// GetPinnedComponentVersion returns the component version ref names from repo and the repository
// to read it through afterwards. If ref is pinned to a digest, the descriptor is verified against
// it and further reads are bound to it (see [repository.GetPinnedComponentVersion]). Use the
// version of the returned descriptor: a reference pinned to an OCI digest may omit the version.
func GetPinnedComponentVersion(ctx context.Context, repo repository.ComponentVersionRepository, ref *compref.Ref) (repository.ComponentVersionRepository, *descruntime.Descriptor, error) {
	pin, err := DigestPin(ref)
	if err != nil {
		return nil, nil, err
	}
	return repository.GetPinnedComponentVersion(ctx, repo, ref.Component, ref.Version, pin)
}

// GetComponentVersionByArtifact is [GetPinnedComponentVersion] that also binds an unpinned (or
// normalized-pinned) read to the artifact the version tag resolves to (see
// [repository.GetComponentVersionByArtifact]). It returns ref pinned to the OCI digest of that
// artifact, which names exactly the returned descriptor, or nil if the repository does not store
// component versions content-addressed.
func GetComponentVersionByArtifact(ctx context.Context, repo repository.ComponentVersionRepository, ref *compref.Ref) (repository.ComponentVersionRepository, *descruntime.Descriptor, *compref.Ref, error) {
	pin, err := DigestPin(ref)
	if err != nil {
		return nil, nil, nil, err
	}
	pinnedRepo, desc, artifact, err := repository.GetComponentVersionByArtifact(ctx, repo, ref.Component, ref.Version, pin)
	if err != nil || artifact == nil {
		return pinnedRepo, desc, nil, err
	}
	pinned := *ref
	pinned.Version = desc.Component.Version
	pinned.Digest = repository.FormatDigestPin(artifact)
	return pinnedRepo, desc, &pinned, nil
}
