package discovery

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/log"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/signing"
)

// digestSpec identifies one digest computation over a descriptor: the same
// descriptor digested under a different normalisation or hash algorithm is a
// different computation.
type digestSpec struct {
	normalisation string
	hash          string
}

// VerifyIntegrity anchors the resolved graph to the digest chain of the root
// component. The root descriptor is checked against rootDigest, the digest the
// Component resource reports for its (optionally signature-verified) component
// version, and every referenced descriptor is checked against the digest its
// parent's reference entry pins. This mirrors the reference-path rule of the
// resolution worker pool: a component descriptor digest is computed over the
// normalised descriptor including its references' digests, so a verified root
// transitively covers every pinned edge of the graph.
//
// A pinned edge must match or the traversal result is rejected. An edge
// without a digest is skipped rather than rejected: an unpinned chain is
// already visible to callers through Graph.DigestsComplete and the Discovery's
// observed component digest, and rejecting it would break repositories that
// never digest their references. With a nil rootDigest only pinned edges are
// checked.
func VerifyIntegrity(ctx context.Context, graph *Graph, root ComponentKey, rootDigest *v2.Digest) error {
	if graph == nil {
		return fmt.Errorf("graph must not be nil")
	}

	byKey := make(map[ComponentKey]*descriptor.Descriptor, len(graph.Descriptors))
	for _, d := range graph.Descriptors {
		byKey[ComponentKey{Name: d.Component.Name, Version: d.Component.Version}] = d
	}

	logger := slog.New(logr.ToSlogHandler(log.FromContext(ctx)))
	// Several parents may pin the same component, and the root anchor may use
	// the same algorithms as a reference pin: compute each digest once.
	computed := make(map[ComponentKey]map[digestSpec]string, len(graph.Descriptors))
	digestFor := func(key ComponentKey, desc *descriptor.Descriptor, spec digestSpec) (string, error) {
		if value, ok := computed[key][spec]; ok {
			return value, nil
		}
		dig, err := signing.GenerateDigest(ctx, desc, logger, spec.normalisation, spec.hash)
		if err != nil {
			return "", fmt.Errorf("failed to generate digest for component version %s: %w", key, err)
		}
		if computed[key] == nil {
			computed[key] = make(map[digestSpec]string, 1)
		}
		computed[key][spec] = dig.Value
		return dig.Value, nil
	}

	if rootDigest != nil && rootDigest.Value != "" {
		rootDesc, ok := byKey[root]
		if !ok {
			return fmt.Errorf("root component version %s is not in the resolved graph", root)
		}
		value, err := digestFor(root, rootDesc, digestSpec{normalisation: rootDigest.NormalisationAlgorithm, hash: rootDigest.HashAlgorithm})
		if err != nil {
			return err
		}
		if value != rootDigest.Value {
			return fmt.Errorf("digest mismatch (%s/%s) for root component version %s: expected %s, got %s",
				rootDigest.NormalisationAlgorithm, rootDigest.HashAlgorithm, root, rootDigest.Value, value)
		}
	}

	for _, parent := range graph.Descriptors {
		parentKey := ComponentKey{Name: parent.Component.Name, Version: parent.Component.Version}
		for i := range parent.Component.References {
			ref := &parent.Component.References[i]
			if ref.Digest.Value == "" {
				continue
			}
			id := ref.ToComponentIdentity()
			childKey := ComponentKey{
				Name:    id[descriptor.IdentityAttributeName],
				Version: id[descriptor.IdentityAttributeVersion],
			}
			child, ok := byKey[childKey]
			if !ok {
				return fmt.Errorf("component version %s pinned by %s is not in the resolved graph", childKey, parentKey)
			}
			value, err := digestFor(childKey, child, digestSpec{normalisation: ref.Digest.NormalisationAlgorithm, hash: ref.Digest.HashAlgorithm})
			if err != nil {
				return err
			}
			if value != ref.Digest.Value {
				return fmt.Errorf("digest mismatch (%s/%s) for component version %s pinned by %s: expected %s, got %s",
					ref.Digest.NormalisationAlgorithm, ref.Digest.HashAlgorithm, childKey, parentKey, ref.Digest.Value, value)
			}
		}
	}

	return nil
}
