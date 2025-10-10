package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

type ListComponentsRequest[T runtime.Typed] struct {
	// Specification of the Repository to list components in.
	Repository T `json:"repository"`

	// The `last` parameter is the value of the last element of the previous page.
	Last string `json:"last"`
}

type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}
type GetIdentityResponse struct {
	Identity map[string]string `json:"identity"`
}
