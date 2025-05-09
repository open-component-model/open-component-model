package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ConsumerIdentityForConfigRequest[T runtime.Typed] struct {
	// The Location of the Component Version
	Config T `json:"config"`
}

type ResolveRequest[T runtime.Typed] struct {
	Config   T                `json:"config"`
	Identity runtime.Identity `json:"identity"`
}
