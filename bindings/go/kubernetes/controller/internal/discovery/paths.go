package discovery

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"cel.dev/cel-go/common/types"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// referencePaths returns, for every component reachable from root in byKey,
// the shortest component reference path from root, usable verbatim as a
// Resource's spec.resource.byReference.referencePath. Root maps to an empty,
// non-nil path. Among equally short paths the lexicographically smallest
// sequence of runtime.Identity.String() values wins. A root missing from byKey
// yields nil.
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
			// versionNeeded must see the component's complete reference list,
			// never a candidate subset, or an element can lose a version it
			// needs to be told apart from a sibling.
			needed := versionNeeded(ids)
			for i := range refs {
				child := ComponentKey{Name: refs[i].Component, Version: refs[i].Version}
				if _, done := paths[child]; done {
					continue
				}
				if d, ok := byKey[child]; !ok || d == nil {
					continue
				}
				element := versionlessIdentity(ids[i], needed)
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

// versionNeeded returns the versionless forms (see identityKey) that occur
// more than once in ids. An element whose versionless identity is in the
// result needs its version to be told apart from the others. ids must be all
// elements the Resource controller matches against, not a filtered subset, or
// an element can lose a version it needs.
func versionNeeded(ids []runtime.Identity) map[string]bool {
	counts := make(map[string]int, len(ids))
	for _, id := range ids {
		counts[identityKey(withoutVersion(id))]++
	}
	needed := map[string]bool{}
	for s, n := range counts {
		if n > 1 {
			needed[s] = true
		}
	}
	return needed
}

// versionlessIdentity returns id without its version attribute, unless needed
// (see versionNeeded) says the version is required to tell it apart. This
// matches how the Resource controller matches identities
// (ocm.IdentityFuncIgnoreVersion): a versionless identity follows whatever
// version the element currently has, so it keeps working after the root
// component moves to a new version.
func versionlessIdentity(id runtime.Identity, needed map[string]bool) runtime.Identity {
	stripped := withoutVersion(id)
	if needed[identityKey(stripped)] {
		if id == nil {
			return runtime.Identity{}
		}
		return id.Clone()
	}
	return stripped
}

// identityKey encodes id so that two identities share a key exactly when they
// are equal. runtime.Identity.String joins key=value pairs with commas, so
// values containing "," or "=" can make different identities collide.
func identityKey(id runtime.Identity) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(id)) {
		// Quoting escapes the separators, so the encoding is unambiguous.
		b.WriteString(strconv.Quote(k))
		b.WriteByte('=')
		b.WriteString(strconv.Quote(id[k]))
		b.WriteByte(',')
	}
	return b.String()
}

func withoutVersion(id runtime.Identity) runtime.Identity {
	s := id.Clone()
	if s == nil {
		s = runtime.Identity{}
	}
	delete(s, descriptor.IdentityAttributeVersion)
	return s
}

// bindings provides the referencePath and resourceIdentity bindings of the
// per-element extraction modes. They are bound lazily: CEL only calls a lazy
// binding when an expression reads the variable, so a Discovery whose
// expressions do not use them pays nothing. Each value is computed once.
type bindings struct {
	filtered *Filtered
	paths    map[ComponentKey][]runtime.Identity // computed on first use
}

// referencePath returns the lazy referencePath binding of d, or nil when the
// graph has no root, which leaves the binding unset: expressions reading it are
// a missing access and their field is omitted.
func (b *bindings) referencePath(d *descriptor.Descriptor) func() any {
	if _, ok := b.filtered.unfiltered[b.filtered.root]; !ok {
		return nil
	}
	key := ComponentKey{Name: d.Component.Name, Version: d.Component.Version}
	return func() any {
		if b.paths == nil {
			b.paths = referencePaths(b.filtered.root, b.filtered.unfiltered)
		}
		path, ok := b.paths[key]
		if !ok {
			return types.NewErr("component %s is not reachable from the root component %s", key, b.filtered.root)
		}
		return path
	}
}

// resourceIdentity returns a function that yields the lazy resourceIdentity
// binding of the j-th of the n v2 resources of d. Whether a version is needed
// is judged against all resources of the component, which is what the
// Resource controller matches against, not only those a resource selector
// kept.
func (b *bindings) resourceIdentity(d *descriptor.Descriptor, n int) func(j int) func() any {
	var needed map[string]bool
	return func(j int) func() any {
		return func() any {
			// The v2 resource list is a conversion of the runtime one, so both
			// share order and length. Pairing by index is only safe while that
			// holds; refuse to guess otherwise.
			if n != len(d.Component.Resources) {
				return types.NewErr("component %s has %d v2 resources but %d runtime resources",
					d.Component.String(), n, len(d.Component.Resources))
			}
			if needed == nil {
				all := d.Component.Resources
				if orig, ok := b.filtered.unfiltered[ComponentKey{Name: d.Component.Name, Version: d.Component.Version}]; ok {
					all = orig.Component.Resources
				}
				ids := make([]runtime.Identity, len(all))
				for k := range all {
					ids[k] = all[k].ToIdentity()
				}
				needed = versionNeeded(ids)
			}
			return versionlessIdentity(d.Component.Resources[j].ToIdentity(), needed)
		}
	}
}
