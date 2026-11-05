// Package typedtest provides [runtime.Typed] values for tests.
//
// [Unregistered] has a type no scheme knows. A table-driven conversion test uses it for the
// case that must fail:
//
//	{name: "unknown type returns error", input: &typedtest.Unregistered{}, wantErr: true},
package typedtest

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Unregistered is a [runtime.Typed] of a type no scheme knows. Pass it to a conversion that
// must reject unknown input types.
type Unregistered struct{}

var _ runtime.Typed = (*Unregistered)(nil)

// GetType implements [runtime.Typed].
func (*Unregistered) GetType() runtime.Type { return runtime.NewUnversionedType("Unknown") }

// SetType implements [runtime.Typed]. The type cannot be changed.
func (*Unregistered) SetType(runtime.Type) {}

// DeepCopyTyped implements [runtime.Typed].
func (*Unregistered) DeepCopyTyped() runtime.Typed { return &Unregistered{} }
