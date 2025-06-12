package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ConsumerIdentityForConfigRequest contains a typed Config that the plugin will understand that is specific for the
// plugin type.
type ConsumerIdentityForConfigRequest[T runtime.Typed] struct {
	// The Location of the Component Version
	Config T `json:"config"`
}

// ResolveRequest contains a specific typed config and identity used for resolving credentials using the credential graph.
type ResolveRequest[T runtime.Typed] struct {
	Config   T                `json:"config"`
	Identity runtime.Identity `json:"identity"`
}
