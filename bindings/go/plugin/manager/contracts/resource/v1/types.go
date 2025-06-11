package v1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type GetResourceRequest struct {
	types.Location
	// The resource specification to download
	*v2.Resource `json:"resource"`
}

type GetResourceResponse struct {
	// Location where the resource will be downloaded to and can be accessed.
	Location types.Location `json:"location"`
}

type PostResourceRequest struct {
	// The ResourceLocation of the Local Resource
	ResourceLocation types.Location `json:"resourceLocation"`
	Resource         *v2.Resource   `json:"resource"`
}

type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}

type GetIdentityResponse struct {
	Identity map[string]string `json:"identity"`
}

type PostResourceResponse struct {
	Resource *v2.Resource `json:"resource"`
}
