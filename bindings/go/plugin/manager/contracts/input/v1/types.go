package v1

import (
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}

type GetIdentityResponse struct {
	Identity map[string]string `json:"identity"`
}

type ProcessResourceInputRequest struct {
	Resource *constructorv1.Resource `json:"resource"`
}

type ProcessResourceResponse struct {
	Resource *descriptorv2.Resource `json:"resource"`
	Location *types.Location        `json:"location"`
}

type ProcessSourceInputRequest struct {
	Source *constructorv1.Source `json:"source"`
}

type ProcessSourceResponse struct {
	Source   *descriptorv2.Source `json:"source"`
	Location *types.Location      `json:"location"`
}
