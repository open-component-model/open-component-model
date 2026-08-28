package runtime

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

var (
	_ Artifact = (*Source)(nil)
	_ Artifact = (*Resource)(nil)
)

// Artifact defines a common interface for both Source and Resource types.
// It provides methods to access common metadata and properties.
type Artifact interface {
	// GetElementMeta returns the element metadata
	GetElementMeta() ElementMeta
	// GetType returns the type of the artifact
	GetType() string
	// GetAccess returns the access information
	GetAccess() runtime.Typed
}

func (s *Source) GetElementMeta() ElementMeta {
	return s.ElementMeta
}

func (s *Source) GetType() string {
	return s.Type
}

func (s *Source) GetAccess() runtime.Typed {
	return s.Access
}

func (r *Resource) GetElementMeta() ElementMeta {
	return r.ElementMeta
}

func (r *Resource) GetType() string {
	return r.Type
}

func (r *Resource) GetAccess() runtime.Typed {
	return r.Access
}

// FindArtifactsByIdentity returns the artifacts from candidates whose identity
// matches the given identity. Exact equality is preferred over subset matching:
// if any artifact's identity exactly equals the lookup identity, only those are
// returned. Otherwise all artifacts whose identity is a superset of the lookup
// identity (subset match) are returned.
//
// This allows callers to use a partial identity (e.g. {name, os, architecture}
// without version) while still resolving correctly when multiple artifacts share
// the same partial keys but differ by additional extraIdentity fields.
func FindArtifactsByIdentity(identity runtime.Identity, candidates []Artifact) []Artifact {
	var exact, subset []Artifact
	for _, a := range candidates {
		meta := a.GetElementMeta()
		id := meta.ToIdentity()
		if identity.Match(id, runtime.IdentityMatchingChainFn(runtime.IdentityEqual)) {
			exact = append(exact, a)
		} else if identity.Match(id, runtime.IdentityMatchingChainFn(runtime.IdentitySubset)) {
			subset = append(subset, a)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return subset
}
