package discovery

import (
	"slices"
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
// The search is a breadth-first search that visits the references of every
// component sorted by their path element. Components are then reached in the
// order of their paths, so the first path found to a component is its
// lexicographically smallest shortest path.
func referencePaths(root ComponentKey, byKey map[ComponentKey]*descriptor.Descriptor) map[ComponentKey][]runtime.Identity {
	if d, ok := byKey[root]; !ok || d == nil {
		return nil
	}

	type hop struct {
		child   ComponentKey
		element runtime.Identity
		key     string
	}

	paths := map[ComponentKey][]runtime.Identity{root: {}}
	queue := []ComponentKey{root}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]

		refs := byKey[parent].Component.References
		ids := make([]runtime.Identity, len(refs))
		for i := range refs {
			ids[i] = refs[i].ToIdentity()
		}
		needed := versionNeeded(ids)
		hops := make([]hop, len(refs))
		for i := range refs {
			element := versionlessIdentity(ids[i], needed)
			hops[i] = hop{
				child:   ComponentKey{Name: refs[i].Component, Version: refs[i].Version},
				element: element,
				key:     element.String(),
			}
		}
		slices.SortStableFunc(hops, func(a, b hop) int { return strings.Compare(a.key, b.key) })

		for _, h := range hops {
			// A component that already has a path was reached by a shorter or
			// smaller one. This also terminates cycles.
			if _, done := paths[h.child]; done {
				continue
			}
			if d, ok := byKey[h.child]; !ok || d == nil {
				continue
			}
			paths[h.child] = append(slices.Clone(paths[parent]), h.element)
			queue = append(queue, h.child)
		}
	}
	return paths
}

// versionNeeded returns the versionless forms (runtime.Identity.String) that
// occur more than once in ids. An element whose versionless identity is in the
// result needs its version to be told apart from the others. ids must be all
// elements the Resource controller matches against, not a filtered subset, or
// an element can lose a version it needs.
func versionNeeded(ids []runtime.Identity) map[string]bool {
	counts := make(map[string]int, len(ids))
	for _, id := range ids {
		counts[withoutVersion(id).String()]++
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
	if needed[stripped.String()] {
		if id == nil {
			return runtime.Identity{}
		}
		return id.Clone()
	}
	return stripped
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
