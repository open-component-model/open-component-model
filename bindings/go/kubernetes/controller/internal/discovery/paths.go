package discovery

import (
	"slices"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// referencePaths returns, for every component reachable from root in byKey,
// the shortest component reference path from root, usable verbatim as a
// Resource's spec.resource.byReference.referencePath. Root maps to an empty,
// non-nil path. Among equally short paths the lexicographically smallest
// sequence of runtime.Identity.String() values wins, so the result does not
// depend on map iteration order. A root missing from byKey yields nil.
//
// Each path element is a reference identity as minimized by
// versionlessIdentities.
func referencePaths(root ComponentKey, byKey map[ComponentKey]*descriptor.Descriptor) map[ComponentKey][]runtime.Identity {
	if d, ok := byKey[root]; !ok || d == nil {
		return nil
	}

	paths := map[ComponentKey][]runtime.Identity{root: {}}
	frontier := []ComponentKey{root}

	// Level-wise BFS: every parent in the frontier has a path of the same
	// length, so every candidate of one level is equally short and the
	// lexicographic comparison alone decides between them.
	for len(frontier) > 0 {
		next := map[ComponentKey][]runtime.Identity{}
		for _, parent := range frontier {
			parentDesc := byKey[parent]
			refs := parentDesc.Component.References
			ids := make([]runtime.Identity, len(refs))
			for i := range refs {
				ids[i] = refs[i].ToIdentity()
			}
			elements := versionlessIdentities(ids)

			for i := range refs {
				child := ComponentKey{Name: refs[i].Component, Version: refs[i].Version}
				// A child that already has a path got it on an earlier, shorter
				// level. This also terminates cycles.
				if _, done := paths[child]; done {
					continue
				}
				if d, ok := byKey[child]; !ok || d == nil {
					continue
				}
				candidate := append(slices.Clone(paths[parent]), elements[i])
				if existing, ok := next[child]; !ok || lessPath(candidate, existing) {
					next[child] = candidate
				}
			}
		}

		frontier = frontier[:0]
		for child, path := range next {
			paths[child] = path
			frontier = append(frontier, child)
		}
	}

	return paths
}

// versionlessIdentities returns ids with the version attribute removed from
// every identity that stays unique without it. An identity keeps its version
// only when another identity in ids is equal to it once both drop their
// version. This matches how the Resource controller matches identities
// (ocm.IdentityFuncIgnoreVersion): a versionless identity follows whatever
// version the element currently has, so a path or resource identity keeps
// working after the root component moves to a new version, while an element
// that needs its version to be unambiguous keeps it.
//
// The input is not modified.
func versionlessIdentities(ids []runtime.Identity) []runtime.Identity {
	stripped := make([]runtime.Identity, len(ids))
	counts := make(map[string]int, len(ids))
	for i, id := range ids {
		s := id.Clone()
		if s == nil {
			s = runtime.Identity{}
		}
		delete(s, descriptor.IdentityAttributeVersion)
		stripped[i] = s
		counts[s.String()]++
	}

	out := make([]runtime.Identity, len(ids))
	for i, s := range stripped {
		if counts[s.String()] > 1 {
			out[i] = ids[i].Clone()
			continue
		}
		out[i] = s
	}
	return out
}

// lessPath orders equally long paths by their element String() values.
func lessPath(a, b []runtime.Identity) bool {
	for k := range a {
		as, bs := a[k].String(), b[k].String()
		if as != bs {
			return as < bs
		}
	}
	return false
}

// identityValue converts an identity into the CEL value bound to extraction
// expressions: a map of string to string.
func identityValue(id runtime.Identity) map[string]any {
	out := make(map[string]any, len(id))
	for k, v := range id {
		out[k] = v
	}
	return out
}

// pathValue converts a reference path into the CEL value bound to extraction
// expressions: a list of maps of string to string. An empty path stays an
// empty, non-nil list.
func pathValue(path []runtime.Identity) []any {
	out := make([]any, 0, len(path))
	for _, id := range path {
		out = append(out, identityValue(id))
	}
	return out
}
