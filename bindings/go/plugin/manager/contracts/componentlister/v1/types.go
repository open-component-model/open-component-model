package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// ListComponentsRequest contains information needed for a single call to an implementing plugin.
type ListComponentsRequest[T runtime.Typed] struct {
	// Specification of the Repository to list components in.
	Repository T `json:"repository"`

	// The `last` parameter is the value of the last element of the previous page.
	// If set, the returned list must start non-inclusively with this value. Without the last query parameter,
	// the list returned will start at the beginning.
	Last string `json:"last"`
}

type ListComponentsResponse struct {
	List   []string                      `json:"list"`
	Header *ListComponentsResponseHeader `json:"header,omitempty"`
}

// ListComponentsResponseHeader must be set by the plug-in in the response when additional components are available.
type ListComponentsResponseHeader struct {
	// The new last, i.e. the value of the last element in the returned list.
	Last string `json:"last,omitempty"`
}

type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}
type GetIdentityResponse struct {
	Identity map[string]string `json:"identity"`
}
