package ocm

import (
	"context"
	"fmt"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	"ocm.software/open-component-model/bindings/go/repository"
)

// DigestPin returns the digest the component reference is pinned to (see
// [repository.ParseDigestPin]), or nil if it carries no digest. A pin identifies exactly
// one component version, so a pinned reference without a version is rejected: it cannot be
// combined with version listing or latest-version resolution.
func DigestPin(ref *compref.Ref) (*descruntime.Digest, error) {
	if ref.Digest == "" {
		return nil, nil
	}
	if ref.Version == "" {
		return nil, fmt.Errorf("component reference %q is pinned to digest %q but has no version: a digest-pinned reference must name the version", ref.String(), ref.Digest)
	}
	pin, err := repository.ParseDigestPin(ref.Digest)
	if err != nil {
		return nil, fmt.Errorf("component reference %q: %w", ref.String(), err)
	}
	return pin, nil
}

// GetPinnedComponentVersion returns the component version ref names from repo. If ref is
// pinned to a digest, the descriptor is verified against it (see [repository.GetPinnedComponentVersion]).
func GetPinnedComponentVersion(ctx context.Context, repo repository.ComponentVersionRepository, ref *compref.Ref) (*descruntime.Descriptor, error) {
	pin, err := DigestPin(ref)
	if err != nil {
		return nil, err
	}
	return repository.GetPinnedComponentVersion(ctx, repo, ref.Component, ref.Version, pin)
}
