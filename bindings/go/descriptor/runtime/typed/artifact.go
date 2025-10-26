// Package typed defines a type-safe, generic wrapper around OCM artifacts (such as Resource and Source)
// to ensure compile-time and runtime correctness when working with runtime.Typed access specifications.
//
// It bridges generic typed access (in this package) and raw untyped artifact definitions (in the runtime package).
// The wrapper guarantees that the Access field on any artifact matches the expected Go type
// and provides safe getter and setter methods.
package typed

import (
	"encoding/json"
	"fmt"

	untyped "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Artifact is a [untyped.Artifact] (e.g. [*untyped.Resource] or [*untyped.Source])
// with a compile-time type parameter for the underlying access in [*untyped.Resource.GetAccess].
//
// Type parameters:
//   - ACCESS: a concrete access type implementing [runtime.Typed] (e.g. *OCIArtifactAccess)
//   - BASE: the untyped artifact type implementing [untyped.Artifact] (e.g. [*untyped.Resource] or [*untyped.Source])
//
// It provides safe getters and setters for common fields and the underlying access specification:
//
//		res := runtime.Resource{Access: &OCIArtifactAccess{}}
//		art, err := typed.FromArtifact[*OCIArtifactAccess](&res)
//		if err != nil {
//	   // handle error
//		}
//		art.Underlying() // operate as you would normally with untyped artifacts
//		art.SetAccess(&HELMChart{}) // compile-time error: ACCESS != *OCIArtifactAccess
type Artifact[ACCESS runtime.Typed, BASE untyped.Artifact] interface {
	Base() BASE
	Typed() Access[ACCESS]
}

type Access[ACCESS runtime.Typed] interface {
	// GetAccess returns the typed access specification (e.g. *OCIArtifactAccess).
	GetAccess() ACCESS
	// SetAccess sets the typed access specification.
	SetAccess(ACCESS)
}

// NewArtifact wraps a raw untyped artifact into a type-safe [Artifact] wrapper.
//
// It performs a runtime type assertion to ensure the artifact's Access field matches
// the expected ACCESS type. If the type does not match, it returns an error.
//
// Example:
//
//	res := runtime.Resource{Access: &OCIArtifactAccess{}}
//	t, err := typed.FromArtifact[*OCIArtifactAccess](&res)
//
//	t.Typed() // returns *OCIArtifactAccess
//	t.SetAccess(&HELMChart{}) // compile-time error: ACCESS != *OCIArtifactAccess
//	t.Base() // returns *runtime.Resource
func NewArtifact[ACCESS runtime.Typed, BASE untyped.Artifact](art BASE) (Artifact[ACCESS, BASE], error) {
	acc := art.GetAccess()
	// Validate that the access type (if present) matches the expected ACCESS type parameter.
	if acc == nil {
		return nil, fmt.Errorf("artifact has no access specification")
	}
	if _, ok := acc.(ACCESS); !ok {
		return nil, fmt.Errorf("found unexpected access type in artifact: got %T, expected %T", acc, *new(ACCESS))
	}
	// Return a new typed wrapper over the artifact.
	return &artifact[ACCESS, BASE]{underlying: art, access: &access[ACCESS]{art}}, nil
}

// artifact implements the generic [Artifact] interface.
//
// It delegates all field access and mutation to the underlying untyped artifact,
// while exposing the Access field as the concrete generic type.
type artifact[ACCESS runtime.Typed, BASE untyped.Artifact] struct {
	underlying BASE
	access     Access[ACCESS]
}

func (r *artifact[ACCESS, BASE]) Base() BASE {
	return r.underlying
}

func (r *artifact[ACCESS, BASE]) Typed() Access[ACCESS] {
	return r.access
}

// MarshalJSON implements json.Marshaler.
func (r artifact[ACCESS, BASE]) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.underlying)
}

func (r *artifact[ACCESS, BASE]) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &r.underlying)
}

type access[ACCESS runtime.Typed] struct {
	underlying untyped.Artifact
}

// GetAccess returns the Access field cast to the generic ACCESS type.
//
// Panics if the underlying Access field does not match the ACCESS type.
// (Which can only happen if the artifact was created through [FromArtifact] and the ACCESS type parameter does
// not match the actual access type after being modified with Untyped.)
// This is safe if the artifact was created through [FromArtifact].
func (r *access[ACCESS]) GetAccess() ACCESS {
	return r.underlying.GetAccess().(ACCESS)
}

// SetAccess assigns a new typed access object to the artifact.
func (r *access[ACCESS]) SetAccess(access ACCESS) {
	r.underlying.SetAccess(access)
}
