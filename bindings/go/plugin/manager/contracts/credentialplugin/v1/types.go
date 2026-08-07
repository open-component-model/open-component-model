package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetConsumerIdentityRequest contains the credential specification for which the consumer identity should be resolved.
type GetConsumerIdentityRequest[T runtime.Typed] struct {
	Credential T `json:"credential"`
}

// ResolveRequest contains the identity for which credentials should be resolved.
type ResolveRequest[T runtime.Typed] struct {
	Identity runtime.Identity `json:"identity"`
}
