package v1

import (
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type TransformBlobRequest[T runtime.Typed] struct {
	// Location of the data to be transformed.
	Location types.Location `json:"location"`
	// Specification of the transformation to be applied to the data.
	Specification T `json:"specification"`
}
type TransformBlobResponse struct {
	// Location of the transformed data.
	Location types.Location `json:"location"`
}

type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}

type GetIdentityResponse struct {
	Identity map[string]string `json:"identity"`
}
