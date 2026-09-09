package internal

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
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

// identityToTransformationID converts a component identity (name + version) to a transformation
// ID suitable for use as a DAG vertex key. The identity map keys are sorted alphabetically for
// determinism and non-alphanumeric characters are escaped for CEL compatibility.
//
// Example: {"name": "ocm.software/my-app", "version": "1.0.0"} → "transform_slash_ocm_dot_software_slash_my_dash_app_slash_1_dot_0_dot_0"
func identityToTransformationID(id runtime.Identity) string {
	words := []string{"transform"}
	keys := make([]string, 0, len(id))
	for k := range id {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		words = append(words, id[k])
	}
	return escapeTransformationID(strings.Join(words, "/"))
}

// Functionality is similar to cel.jsonschema.Escape, but there are some key differences:
//   - `Escape` has a failure path, our id mapping is expected to always succeed
//   - `Escape` also contains validation logic, our input has already been validated
//   - `Escape` also guards against reserved keywords being used, we always have a version and
//     thus a number in the identifier, thereby never matching a keyword exactly
//   - `Escape` is based on k8s (https://github.com/kubernetes/kubernetes/blob/0c3b1fdd96c940faa3ec43094343d6e44554b74d/staging/src/k8s.io/apiserver/pkg/cel/escaping.go#L90-L136)
//     but the allowed characters in id & version differ from the supported input format: [a-zA-Z_.-/][a-zA-Z0-9_.-/]*
//   - Our id mapping needs to support all characters allowed in `componentName`, `identityAttributeKey`, and `relaxedSemver`,
//     which boils down to: [a-zA-Z0-9.\-_/+] (only the + is different)
//   - Transformation IDs dictate the names of their corresponding runtime objects, but
//     these objects are escaped again in `NewDeclType`. So we don't want to use the
//     double underscore escape sequence, because that would lead to e.g.
//     `.` -> `__dot__` -> `__underscores__dot__underscores__`
var expandMatcher = regexp.MustCompile(`(_|[-./+])`)

func escapeTransformationID(ident string) string {
	ident = expandMatcher.ReplaceAllStringFunc(ident, func(s string) string {
		switch s {
		case "_":
			return "__"
		case ".":
			return "_dot_"
		case "-":
			return "_dash_"
		case "+":
			return "_plus_"
		case "/":
			return "_slash_"
		}
		return s
	})
	return ident
}

// resourceIDAllocator hands out unique transformation IDs for the resources of one
// component. identityToTransformationID is lossy: it drops separators and folds case, so
// distinct identities (for example versions "0.2.1+meta" and "0.2.1-meta") map to the same
// base ID. On collision the allocator appends an incrementing suffix ("R1", "R2", ...),
// mirroring the per-target "T0", "T1" suffix scheme.
type resourceIDAllocator struct {
	// used contains every ID handed out so far, including suffixed candidates.
	used map[string]struct{}
	// nextSuffix tracks the first untried suffix per colliding base ID.
	nextSuffix map[string]int
}

func newResourceIDAllocator() *resourceIDAllocator {
	return &resourceIDAllocator{
		used:       make(map[string]struct{}),
		nextSuffix: make(map[string]int),
	}
}

// allocate returns base unchanged while it is unused. On collision, it returns the first
// unused ID of the form base+"R"+counter, with the counter starting at 1. Callers must
// allocate in a deterministic order (descriptor order) to keep the resulting IDs stable.
func (a *resourceIDAllocator) allocate(base string) string {
	if _, ok := a.used[base]; !ok {
		a.used[base] = struct{}{}
		return base
	}
	for n := max(a.nextSuffix[base], 1); ; n++ {
		candidate := fmt.Sprintf("%sR%d", base, n)
		if _, ok := a.used[candidate]; !ok {
			a.used[candidate] = struct{}{}
			a.nextSuffix[base] = n + 1
			return candidate
		}
	}
}
