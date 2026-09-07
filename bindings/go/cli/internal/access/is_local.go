package access

import (
	"ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// IsLocal checks if access method is local
func IsLocal(access runtime.Typed) bool {
	if access == nil {
		return false
	}
	var local v2.LocalBlob
	if err := v2.Scheme.Convert(access, &local); err != nil {
		return false
	}
	return true
}
