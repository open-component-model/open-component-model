package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2/content"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/oci/spec"
	ocidescriptor "ocm.software/open-component-model/bindings/go/oci/spec/descriptor"
	normalizedlayout "ocm.software/open-component-model/bindings/go/oci/spec/layout/normalized/v1"
)

// IsNormalizedManifest reports whether a resolved manifest descriptor is the normalized-layout tag target.
func IsNormalizedManifest(target ociImageSpecV1.Descriptor) bool {
	return target.ArtifactType == ocidescriptor.ArtifactTypeNormalizedDescriptor
}

// GetNormalizedComponentVersion resolves the full access-bearing descriptor for a normalized
// manifest and returns it only after the bind check passes. Selection is safety-first: only
// bind-check survivors are trusted; survivors are ordered by their own manifest digest; resolution
// falls through to the next survivor on any failure.
func GetNormalizedComponentVersion(ctx context.Context, store spec.Store, normalized ociImageSpecV1.Descriptor, unmarshal ocidescriptor.UnmarshalFunc) (*descriptor.Descriptor, error) {
	// Fetch the normalized (signed) manifest and extract its single layer bytes.
	normManifestRaw, err := content.FetchAll(ctx, store, normalized)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch normalized manifest: %w", err)
	}
	var normManifest ociImageSpecV1.Manifest
	if err := json.Unmarshal(normManifestRaw, &normManifest); err != nil {
		return nil, fmt.Errorf("failed to unmarshal normalized manifest: %w", err)
	}
	if len(normManifest.Layers) != 1 {
		return nil, fmt.Errorf("normalized manifest %s must have exactly one layer, got %d", normalized.Digest, len(normManifest.Layers))
	}
	signedNormalized, err := content.FetchAll(ctx, store, normManifest.Layers[0])
	if err != nil {
		return nil, fmt.Errorf("failed to fetch normalized descriptor layer: %w", err)
	}

	// Gather candidate access manifests from referrers and the predictable fallback tag, deduped.
	seen := make(map[string]bool)
	var candidates []ociImageSpecV1.Descriptor
	var discoveryErr error

	if predecessors, predecessorsErr := store.Predecessors(ctx, normalized); predecessorsErr != nil {
		discoveryErr = predecessorsErr
	} else {
		for _, p := range predecessors {
			key := p.Digest.String()
			if !seen[key] {
				seen[key] = true
				candidates = append(candidates, p)
			}
		}
	}

	if fallback, resolveErr := store.Resolve(ctx, normalizedlayout.AccessFallbackTag(normalized.Digest.String())); resolveErr != nil {
		// Preserve the fallback error only if we haven't already recorded a discovery error;
		// both are non-fatal as long as the other source yields candidates.
		if discoveryErr == nil {
			discoveryErr = resolveErr
		}
	} else {
		key := fallback.Digest.String()
		if !seen[key] {
			seen[key] = true
			candidates = append(candidates, fallback)
		}
	}

	// Keep only genuine access.v1 referrers whose subject is exactly the normalized manifest.
	type keptCandidate struct {
		desc     ociImageSpecV1.Descriptor
		manifest ociImageSpecV1.Manifest
	}
	var kept []keptCandidate
	for _, c := range candidates {
		raw, err := content.FetchAll(ctx, store, c)
		if err != nil {
			continue
		}
		var man ociImageSpecV1.Manifest
		if err := json.Unmarshal(raw, &man); err != nil {
			continue
		}
		if man.ArtifactType != ocidescriptor.ArtifactTypeAccessDescriptor {
			continue
		}
		if man.Subject == nil || man.Subject.Digest != normalized.Digest {
			continue
		}
		kept = append(kept, keptCandidate{desc: c, manifest: man})
	}

	if len(kept) == 0 {
		if discoveryErr != nil {
			return nil, fmt.Errorf("no access.v1 referrer found for normalized manifest %s: %w", normalized.Digest, discoveryErr)
		}
		return nil, fmt.Errorf("no access.v1 referrer found for normalized manifest %s", normalized.Digest)
	}

	// Deterministic, attacker-proof ordering by the referrer's own manifest digest.
	sort.Slice(kept, func(i, j int) bool {
		return kept[i].desc.Digest.String() < kept[j].desc.Digest.String()
	})

	var lastErr error
	for _, k := range kept {
		if len(k.manifest.Layers) < 1 {
			lastErr = fmt.Errorf("access manifest %s has no layers", k.desc.Digest)
			continue
		}
		layer := k.manifest.Layers[0]
		layerBytes, err := content.FetchAll(ctx, store, layer)
		if err != nil {
			lastErr = fmt.Errorf("failed to fetch access descriptor layer for %s: %w", k.desc.Digest, err)
			continue
		}
		full, err := ocidescriptor.SingleFileDecodeDescriptor(bytes.NewReader(layerBytes), layer.MediaType, unmarshal)
		if err != nil {
			lastErr = fmt.Errorf("failed to decode access descriptor for %s: %w", k.desc.Digest, err)
			continue
		}
		if err := normalizedlayout.VerifyNormalizedMatchesAccess(signedNormalized, full); err != nil {
			lastErr = fmt.Errorf("bind check failed for access manifest %s: %w", k.desc.Digest, err)
			continue
		}
		return full, nil
	}

	return nil, fmt.Errorf("no valid access.v1 referrer for normalized manifest %s: %w", normalized.Digest, lastErr)
}

// peekArtifactType fetches a manifest/index and returns its artifactType from the body. This is
// needed because registries do not populate Descriptor.ArtifactType when resolving a tag; the value
// lives inside the manifest JSON.
func peekArtifactType(ctx context.Context, store spec.Store, target ociImageSpecV1.Descriptor) (string, error) {
	raw, err := content.FetchAll(ctx, store, target)
	if err != nil {
		return "", err
	}
	var peek struct {
		ArtifactType string `json:"artifactType"`
	}
	if err := json.Unmarshal(raw, &peek); err != nil {
		return "", err
	}
	return peek.ArtifactType, nil
}
