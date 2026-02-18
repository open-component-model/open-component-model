package v2

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// IsLocalBlob checks if access method is a v2.LocalBlob
func IsLocalBlob(access runtime.Typed) bool {
	if access == nil {
		return false
	}
	var local LocalBlob
	if err := Scheme.Convert(access, &local); err != nil {
		return false
	}
	return true
}
