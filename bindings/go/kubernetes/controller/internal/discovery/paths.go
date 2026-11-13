package discovery

import (
	"slices"
	"strings"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/ocm"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// referencePaths returns, for every component reachable from root in byKey,
// the shortest component reference path from root, usable verbatim as a
// Resource's spec.resource.byReference.referencePath. Root maps to an empty,
// non-nil path. Only references that an identity selects on their own (see
// ocm.AddressableIdentities) are followed. Among equally short paths the
// lexicographically smallest sequence of runtime.Identity.String() values
// wins. A root missing from byKey yields nil.
//
// The search is a breadth-first search processed one complete layer at a time,
// so every component in a layer is reached by a path of the same length. The
// winner among equally short paths is the one whose complete sequence of
// runtime.Identity.String() values is smallest, which can depend on hops many
// layers up: two parents in the same layer may carry prefixes that serialise
// identically, so ordering a single parent's outgoing references in isolation
// is not enough to order their descendants.
//
// Each frontier entry therefore carries a rank for its already-fixed path's
// string sequence: equal ranks mean equal complete sequences, increasing ranks
// mean increasing sequences. A layer gathers the outgoing hops of every
// frontier entry, sorts them by (parent rank, final-hop string), and accepts
// the first hop reaching each not-yet-seen component. Visiting candidates in
// (parent rank, hop) order is exactly visiting complete candidate paths in
// String-sequence order, so first encounter wins the smallest path. The next
// layer's ranks are assigned by grouping accepted hops with an equal
// (parent rank, hop) pair: such hops have equal complete sequences and must
// share a rank even when they reach distinct components. The rank avoids
// allocating or re-serialising whole paths just to compare them.
func referencePaths(root ComponentKey, byKey map[ComponentKey]*descriptor.Descriptor) map[ComponentKey][]runtime.Identity {
	if d, ok := byKey[root]; !ok || d == nil {
		return nil
	}

	type frontierEntry struct {
		component ComponentKey
		rank      int
	}

	type hop struct {
		parent     ComponentKey
		parentRank int
		child      ComponentKey
		element    runtime.Identity
		key        string
	}

	paths := map[ComponentKey][]runtime.Identity{root: {}}
	frontier := []frontierEntry{{component: root}}
	var candidates []hop
	for len(frontier) > 0 {
		// Gather the outgoing hops of the whole frontier before accepting any
		// child, so children of different parents are ordered against each
		// other and not just within one parent.
		candidates = candidates[:0]
		for _, entry := range frontier {
			refs := byKey[entry.component].Component.References
			ids := make([]runtime.Identity, len(refs))
			for i := range refs {
				ids[i] = refs[i].ToIdentity()
			}
			// Judged against the component's complete reference list, never a
			// candidate subset, which is what the Resource controller matches.
			elements := ocm.AddressableIdentities(ids)
			for i := range refs {
				child := ComponentKey{Name: refs[i].Component, Version: refs[i].Version}
				if _, done := paths[child]; done {
					continue
				}
				if d, ok := byKey[child]; !ok || d == nil {
					continue
				}
				// No identity selects this reference alone, so no hop leads
				// through it.
				element := elements[i]
				if element == nil {
					continue
				}
				candidates = append(candidates, hop{
					parent:     entry.component,
					parentRank: entry.rank,
					child:      child,
					element:    element,
					key:        element.String(),
				})
			}
		}

		slices.SortStableFunc(candidates, func(a, b hop) int {
			if a.parentRank != b.parentRank {
				if a.parentRank < b.parentRank {
					return -1
				}
				return 1
			}
			return strings.Compare(a.key, b.key)
		})

		frontier = frontier[:0]
		rank := -1
		var prevParentRank int
		var prevKey string
		for _, h := range candidates {
			// An earlier, smaller candidate in this layer already claimed the
			// child. This also terminates cycles.
			if _, done := paths[h.child]; done {
				continue
			}
			// Accepted candidates sharing a (parent rank, hop) pair have equal
			// complete sequences and share a rank, even reaching distinct
			// children; skipped candidates never shift the grouping.
			if rank < 0 || h.parentRank != prevParentRank || h.key != prevKey {
				rank++
				prevParentRank = h.parentRank
				prevKey = h.key
			}
			paths[h.child] = append(slices.Clone(paths[h.parent]), h.element)
			frontier = append(frontier, frontierEntry{component: h.child, rank: rank})
		}
	}
	return paths
}

// bindings provides the referencePath and resourceIdentity bindings of the
// per-element extraction modes. A binding that no expression reads is never
// computed. An element without a value leaves its binding unset, so an
// expression reading it is a missing access and its field is omitted.
type bindings struct {
	filtered           *Filtered
	paths              map[ComponentKey][]runtime.Identity // nil unless referencePath is read
	resourceIdentities bool                                // whether resourceIdentity is read
}

func newBindings(filtered *Filtered, reads map[string]bool) *bindings {
	b := &bindings{filtered: filtered, resourceIdentities: reads[bindingResourceIdentity]}
	if reads[bindingReferencePath] {
		b.paths = referencePaths(filtered.root, filtered.unfiltered)
	}
	return b
}

// referencePath returns the reference path of d, or false when it is not read
// or no path from the root addresses d.
func (b *bindings) referencePath(d *descriptor.Descriptor) ([]runtime.Identity, bool) {
	path, ok := b.paths[ComponentKey{Name: d.Component.Name, Version: d.Component.Version}]
	return path, ok
}

// resourceIdentity returns the resourceIdentity binding of each resource of d,
// nil where it is not read or no identity selects the resource alone. That is
// judged against all resources of the component, which is what the Resource
// controller matches against, not only those a resource selector kept.
func (b *bindings) resourceIdentity(d *descriptor.Descriptor) []runtime.Identity {
	if !b.resourceIdentities {
		return nil
	}
	all := d.Component.Resources
	if orig, ok := b.filtered.unfiltered[ComponentKey{Name: d.Component.Name, Version: d.Component.Version}]; ok {
		all = orig.Component.Resources
	}
	ids := make([]runtime.Identity, len(all))
	for k := range all {
		ids[k] = all[k].ToIdentity()
	}
	addressable := ocm.AddressableIdentities(ids)

	// A resource selector keeps resources in order, so the kept ones are a
	// subsequence of all. Equal identities are all unaddressable, so pairing
	// one with the wrong twin changes nothing.
	out := make([]runtime.Identity, len(d.Component.Resources))
	k := 0
	for j := range d.Component.Resources {
		id := d.Component.Resources[j].ToIdentity()
		for k < len(ids) && !ids[k].Equal(id) {
			k++
		}
		if k == len(ids) {
			break
		}
		out[j] = addressable[k]
		k++
	}
	return out
}
