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
	SetType(string)
	// GetAccess returns the access information
	GetAccess() runtime.Typed
	// SetAccess sets the access information
	SetAccess(runtime.Typed)
}

func (s *Source) GetElementMeta() ElementMeta {
	return s.ElementMeta
}

func (s *Source) GetType() string {
	return s.Type
}

func (s *Source) SetType(t string) {
	s.Type = t
}

func (s *Source) GetAccess() runtime.Typed {
	return s.Access
}

func (s *Source) SetAccess(access runtime.Typed) {
	s.Access = access
}

func (r *Resource) GetElementMeta() ElementMeta {
	return r.ElementMeta
}

func (r *Resource) GetType() string {
	return r.Type
}

func (r *Resource) SetType(t string) {
	r.Type = t
}

func (r *Resource) GetAccess() runtime.Typed {
	return r.Access
}

func (r *Resource) SetAccess(access runtime.Typed) {
	r.Access = access
}
