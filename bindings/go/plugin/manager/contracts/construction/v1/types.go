package v1

import (
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
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
	Resource *constructor.Resource `json:"resource"`
}

type ProcessResourceResponse struct {
	Resource *constructor.Resource `json:"resource"`
	Location *types.Location       `json:"location"`
}

type ProcessSourceInputRequest struct {
	Source *constructor.Source `json:"source"`
}

type ProcessSourceResponse struct {
	Source   *constructor.Source `json:"source"`
	Location *types.Location     `json:"location"`
}

type ProcessResourceDigestRequest struct {
	Resource *descriptor.Resource `json:"resource"`
}

type ProcessResourceDigestResponse struct {
	Resource *descriptor.Resource `json:"resource"`
}
