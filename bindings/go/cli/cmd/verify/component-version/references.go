package componentversion

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
)

// descriptorResolver fetches the descriptor of a component version.
type descriptorResolver func(ctx context.Context, component, version string) (*descruntime.Descriptor, error)

// verifyReferences follows the component references of root recursively. Every referenced
// component version must resolve, and its descriptor must match the digest the referencing
// component stores for it. This is what the spec verification procedure and OCM v1 do:
// a signature only covers a reference through that digest, so without resolving the
// referenced version nothing proves it still exists unchanged.
func verifyReferences(ctx context.Context, logger *slog.Logger, root *descruntime.Descriptor, resolve descriptorResolver) error {
	rootKey := root.Component.Name + ":" + root.Component.Version
	return verifyReferencesOf(ctx, logger, root, resolve, map[string]struct{}{rootKey: {}}, rootKey)
}

func verifyReferencesOf(ctx context.Context, logger *slog.Logger, parent *descruntime.Descriptor, resolve descriptorResolver, visited map[string]struct{}, parentKey string) error {
	for _, ref := range parent.Component.References {
		key := ref.Component + ":" + ref.Version
		child, err := resolve(ctx, ref.Component, ref.Version)
		if err != nil {
			return fmt.Errorf("failed resolving component reference %s[%s] of %s: %w", ref.Name, key, parentKey, err)
		}
		if ref.Digest.Value == "" {
			return fmt.Errorf("component reference %s[%s] of %s carries no digest, so the referenced component version cannot be verified", ref.Name, key, parentKey)
		}
		if err := signing.VerifyDigestMatchesDescriptor(ctx, child, descruntime.Signature{Name: ref.Name, Digest: ref.Digest}, logger); err != nil {
			if errors.Is(err, algorithms.ErrDigestMismatch) {
				return fmt.Errorf("component version %s referenced by %s does not match the digest stored in the reference: %w", key, parentKey, err)
			}
			// Unsupported hash or normalisation algorithms cannot prove the reference unchanged either.
			return fmt.Errorf("component version %s referenced by %s cannot be checked against the digest stored in the reference: %w", key, parentKey, err)
		}
		algorithms.LogNonDefaultReferenceDigest(ctx, logger, slog.LevelWarn, ref.Digest,
			"reference", ref.Name, "component", ref.Component, "version", ref.Version)
		if _, seen := visited[key]; seen {
			continue
		}
		visited[key] = struct{}{}
		logger.DebugContext(ctx, "verified component reference", "reference", ref.Name, "component", ref.Component, "version", ref.Version)
		if err := verifyReferencesOf(ctx, logger, child, resolve, visited, key); err != nil {
			return err
		}
	}
	return nil
}
