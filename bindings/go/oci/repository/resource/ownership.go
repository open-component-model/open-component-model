package resource

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/oci/looseref"
	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/ownership"
	"oras.land/oras-go/v2/registry"
	"oras.land/oras-go/v2/registry/remote"
)

// LookupOwners returns the ownership referrers attached to imageRef via the
// OCI Distribution Referrers API (ADR 0016). imageRef must include a tag or
// digest; an explicit http:// scheme forces plain HTTP. client carries auth;
// pass nil for the ORAS default (anonymous).
//
// Most callers should use [LookupResourceOwners], which builds the
// authenticated client from typed credentials.
func LookupOwners(ctx context.Context, imageRef string, client remote.Client) ([]ownership.Ownership, error) {
	ref, err := looseref.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("parsing image reference %q: %w", imageRef, err)
	}
	return lookupOwners(ctx, ref, imageRef, client)
}

// lookupOwners is the parsed-ref variant used by both [LookupOwners] (which
// parses the string up front) and by the wrapper in [ResourceRepository]
// (which already holds the parsed reference for credential setup). imageRef
// is carried only for error messages so callers see the input that produced
// the failure.
func lookupOwners(ctx context.Context, ref looseref.LooseReference, imageRef string, client remote.Client) ([]ownership.Ownership, error) {
	if ref.Reference.Reference == "" {
		return nil, fmt.Errorf("image reference %q must include a tag or digest to look up ownership", imageRef)
	}

	repo := &remote.Repository{
		Reference:       ref.Reference,
		Client:          client,
		SkipReferrersGC: true, // discovery is read-only; never delete or GC referrers.
		PlainHTTP:       ref.Scheme == "http",
	}

	subject, err := repo.Resolve(ctx, ref.Reference.Reference)
	if err != nil {
		return nil, fmt.Errorf("resolving subject manifest %s: %w", imageRef, err)
	}

	refs, err := registry.Referrers(ctx, repo, subject, annotations.OwnershipArtifactType)
	if err != nil {
		return nil, fmt.Errorf("listing ownership referrers for subject %s: %w", subject.Digest, err)
	}
	out := make([]ownership.Ownership, 0, len(refs))
	for _, desc := range refs {
		parsed, err := ownership.Parse(desc)
		if err != nil {
			if errors.Is(err, ownership.ErrNotAnOwnershipReferrer) {
				slog.DebugContext(ctx, "skipping referrer with ownership artifact type but missing ownership annotations",
					"digest", desc.Digest.String(), "subject", subject.Digest.String())
				continue
			}
			return nil, fmt.Errorf("parsing ownership referrer %s: %w", desc.Digest, err)
		}
		out = append(out, parsed)
	}
	return out, nil
}
