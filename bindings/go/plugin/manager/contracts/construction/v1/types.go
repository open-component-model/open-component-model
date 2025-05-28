package v1

import (
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}

type ProcessResourceRequest struct {
	Resource *constructorv1.Resource `json:"resource"`
}

type ProcessResourceResponse struct {
	Resource *constructorv1.Resource `json:"resource,omitempty"`
	Location *types.Location         `json:"location,omitempty"`
}

type ProcessSourceRequest struct {
	Source *constructorv1.Source `json:"source"`
}

type ProcessSourceResponse struct {
	Source   *constructorv1.Source `json:"source,omitempty"`
	Location *types.Location       `json:"location,omitempty"`
}
