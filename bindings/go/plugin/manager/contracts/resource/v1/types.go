package v1

import (
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type GetGlobalResourceRequest struct {
	// The resource specification to download
	Resource *v2.Resource `json:"resource"`
}
type GetGlobalResourceResponse struct {
	// Location of the data downloaded based on the GetGlobalResourceRequest.Resource specification.
	Location types.Location `json:"location"`
}

type AddGlobalResourceRequest struct {
	// The ResourceLocation of the Local data to be uploaded under the given Resource specification.
	ResourceLocation types.Location `json:"resourceLocation"`
	// Resource specification that describes the resource that should be uploaded.
	Resource *v2.Resource `json:"resource"`
}

type AddGlobalResourceResponse struct {
	// Resource specification that describes the resource after it was uploaded.
	Resource *v2.Resource `json:"resource"`
}

type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}

type GetIdentityResponse struct {
	Identity map[string]string `json:"identity"`
}
