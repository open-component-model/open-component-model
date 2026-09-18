package internal

import (
	"context"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	transferv1alpha1 "ocm.software/open-component-model/bindings/go/transfer/v1alpha1/spec"
)

// discoveryValue holds the result of resolving a single component version during DAG discovery.
// It pairs the fetched descriptor with the repository specification it was resolved from,
// so that downstream graph construction knows which source repository to create Get transformations against.
type discoveryValue struct {
	Descriptor       *descriptor.Descriptor
	SourceRepository runtime.Typed
}

// multiResolver dispatches component version resolution to per-component resolvers.
// Each component key ("component:version") maps to its own resolver, allowing different
// components to be fetched from different source repositories in the same transfer graph.
//
// The resolverMap is shared with the discoverer so that recursively discovered children
// inherit their parent's resolver (see discoverer.Discover).
type multiResolver struct {
	mu             *sync.Mutex // shared with discoverer to protect resolverMap
	resolverMap    map[string]resolvers.ComponentVersionRepositoryResolver
	expectedDigest func(id runtime.Identity) *descriptor.Digest
}

// Resolve fetches the component descriptor for the given key from the appropriate resolver.
// It supports two resolution paths:
//   - If GetRepositorySpecificationForComponent returns a non-nil spec, it opens the repository
//     via GetComponentVersionRepositoryForSpecification (the standard path for full resolvers).
//   - If the spec is nil (e.g., when using FromRepository which wraps a concrete repo directly),
//     it falls back to GetComponentVersionRepositoryForComponent.
//
// After fetching the descriptor, it optionally verifies the digest against the expected value
// recorded during recursive discovery of parent component references.
func (r *multiResolver) Resolve(ctx context.Context, key string) (*discoveryValue, error) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid key format %q: expected component:version", key)
	}
	component, version := parts[0], parts[1]

	slog.DebugContext(ctx, "resolving component version", "component", component, "version", version)

	// Lock to safely read resolverMap which may be mutated by concurrent discovery.
	r.mu.Lock()
	repoResolver, ok := r.resolverMap[key]
	r.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("no resolver found for component %s", key)
	}

	repoSpec, err := repoResolver.GetRepositorySpecificationForComponent(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed getting repository spec for component %s:%s: %w", component, version, err)
	}

	var repo repository.ComponentVersionRepository

	if repoSpec != nil {
		slog.DebugContext(ctx, "resolving via repository specification",
			"component", component, "version", version, "repoSpec", fmt.Sprintf("%T", repoSpec))
		resolved, err := repoResolver.GetComponentVersionRepositoryForSpecification(ctx, repoSpec)
		if err != nil {
			return nil, fmt.Errorf("failed getting component version repository for spec %v: %w", repoSpec, err)
		}
		repo = resolved
	} else {
		slog.DebugContext(ctx, "resolving via direct repository lookup",
			"component", component, "version", version)
		resolved, err := repoResolver.GetComponentVersionRepositoryForComponent(ctx, component, version)
		if err != nil {
			return nil, fmt.Errorf("failed getting component version repository for %s:%s: %w", component, version, err)
		}
		repo = resolved
	}

	desc, err := repo.GetComponentVersion(ctx, component, version)
	if err != nil {
		return nil, fmt.Errorf("failed getting component version %s:%s: %w", component, version, err)
	}

	slog.DebugContext(ctx, "resolved component version",
		"component", component, "version", version,
		"resources", len(desc.Component.Resources),
		"references", len(desc.Component.References))

	if expected := r.expectedDigest(desc.Component.ToIdentity()); expected != nil {
		slog.DebugContext(ctx, "verifying digest for resolved descriptor",
			"component", component, "version", version,
			"expectedDigest", expected.Value)
		if err := signing.VerifyDigestMatchesDescriptor(
			ctx, desc, descriptor.Signature{Digest: *expected}, slog.Default(),
		); err != nil {
			return nil, fmt.Errorf("failed verifying resolved descriptor matches expected digest: %w", err)
		}
	}

	return &discoveryValue{
		Descriptor:       desc,
		SourceRepository: repoSpec,
	}, nil
}

// discoverer implements the dagsync.Discoverer interface for recursive component reference traversal.
// Given a resolved parent component, it extracts child references from the descriptor and returns
// their keys for further resolution by the DAG graph discoverer.
//
// During discovery, it propagates two pieces of state from parent to child:
//   - targetMap: children inherit their parent's transfer targets, so recursively discovered
//     components are transferred to the same repositories as their root ancestor.
//   - resolverMap: children inherit their parent's resolver, so they are fetched from the same
//     source as the root that referenced them.
//
// Both maps are shared with BuildGraphDefinition and the multiResolver, forming the single
// source of truth for target and resolver assignment across the entire discovery graph.
//
// If a child component is referenced by multiple parents with different targets, the child
// accumulates all targets (union). For resolvers, the first parent to claim the child wins.
//
// Thread safety: all map mutations are guarded by mu since the DAG discoverer runs concurrently.
type discoverer struct {
	mu        sync.Mutex
	recursive transferv1alpha1.Recursive

	// discoveredDigests stores expected digests from component references.
	// When a parent references a child with a pinned digest, the digest is recorded here
	// and later verified by the multiResolver after fetching the child descriptor.
	discoveredDigests map[string]descriptor.Digest

	// targetMap tracks which target repositories each component should be transferred to.
	// Seeded from explicit TransferRoots and propagated to children during Discover.
	targetMap map[string][]runtime.Typed

	// resolverMap tracks which resolver to use for each component.
	// Seeded from explicit TransferRoots and propagated to children during Discover.
	resolverMap map[string]resolvers.ComponentVersionRepositoryResolver
}

// Discover extracts component references from a resolved parent and returns their keys
// for recursive resolution. Returns nil if recursive mode is disabled.
//
// For each child reference, it:
//  1. Records the expected digest (if pinned) for later verification.
//  2. Propagates the parent's target repositories to the child (union merge).
//  3. Propagates the parent's resolver to the child. If the child is already claimed by
//     another parent with a different resolver, an error is returned — the ambiguity must
//     be resolved by the caller via an explicit Mapping for that component.
func (d *discoverer) Discover(ctx context.Context, parent *discoveryValue) ([]string, error) {
	if d.recursive == transferv1alpha1.RecursiveNone {
		return nil, nil
	}

	parentKey := parent.Descriptor.Component.Name + ":" + parent.Descriptor.Component.Version

	if len(parent.Descriptor.Component.References) == 0 {
		slog.DebugContext(ctx, "no component references to discover", "parent", parentKey)
		return nil, nil
	}

	slog.DebugContext(ctx, "discovering component references",
		"parent", parentKey,
		"references", len(parent.Descriptor.Component.References))

	var children []string
	for _, ref := range parent.Descriptor.Component.References {
		key := ref.Component + ":" + ref.Version

		slog.DebugContext(ctx, "discovered child reference",
			"parent", parentKey, "child", key,
			"hasDigest", ref.Digest.Value != "")

		// Record expected digest for later verification during resolution.
		if ref.Digest.Value != "" {
			d.mu.Lock()
			d.discoveredDigests[ref.ToComponentIdentity().String()] = ref.Digest
			d.mu.Unlock()
		}

		d.mu.Lock()
		// Propagate parent's target repositories to child (union).
		if d.targetMap != nil {
			parentTargets := d.targetMap[parentKey]
			d.targetMap[key] = AppendUniqueRepositories(d.targetMap[key], parentTargets)
			slog.DebugContext(ctx, "propagated targets to child",
				"child", key, "targets", len(d.targetMap[key]))
		}
		// Propagate parent's resolver to child.
		// If the child already has a resolver from a different parent, fail hard rather than
		// silently picking one: the same child component referenced by two roots with different
		// resolvers is ambiguous (one source must win, but which is non-deterministic under
		// concurrent discovery). The caller should ensure each component is reachable via only
		// one resolver, or explicitly transfer it as its own root with a dedicated mapping.
		if d.resolverMap != nil {
			parentResolver := d.resolverMap[parentKey]
			if existing, exists := d.resolverMap[key]; exists {
				if existing != parentResolver {
					d.mu.Unlock()
					return nil, fmt.Errorf(
						"ambiguous resolver for component %q: referenced by %q (which has resolver %T) "+
							"but already claimed by another parent with a different resolver (%T); "+
							"add an explicit WithTransfer mapping for %q to resolve the ambiguity",
						key, parentKey, parentResolver, existing, key,
					)
				}
			} else {
				d.resolverMap[key] = parentResolver
				slog.DebugContext(ctx, "propagated resolver to child",
					"parent", parentKey, "child", key)
			}
		}
		d.mu.Unlock()

		children = append(children, key)
	}
	return children, nil
}

// identityToTransformationID derives a stable, opaque transformation ID from a component or
// resource identity. The identity map keys are sorted alphabetically and the key/value pairs
// are serialized with NUL separators, then hashed with SHA-256. The sorting makes the digest
// independent of map iteration order, and the NUL separators prevent key/value pairs from
// combining into the same byte stream, e.g. {"ab": "c"} and {"a": "bc"}.
//
// IDs must satisfy (^[a-z][a-zA-Z0-9]*$), so we prefix with a 't' and use base32 for the
// rest of the ID. We use 8 bytes of the 32 byte digest to not explode the ID length.
// 8 bytes of entropy is plenty, collision probability ~1 in 10⁹ chance at around 6000 nodes
//
// Example: {"name": "ocm.software/my-app", "version": "1.0.0"} -> "tamgtopseg3tay"
func identityToTransformationID(id runtime.Identity) string {
	keys := slices.Sorted(maps.Keys(id))
	hash := sha256.New()
	for _, key := range keys {
		hash.Write([]byte(key))
		hash.Write([]byte{0})
		hash.Write([]byte(id[key]))
		hash.Write([]byte{0})
	}
	return "t" + base32Lower.EncodeToString(hash.Sum(nil)[:8])
}

// Standard base32 is shouty due to uppercase characters, use lowercase instead, no other difference
var base32Lower = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)
