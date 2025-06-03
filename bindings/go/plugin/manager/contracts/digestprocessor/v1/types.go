package v1

import (
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}

type GetIdentityResponse struct {
	Identity map[string]string `json:"identity"`
}

type ProcessResourceDigestRequest struct {
	Resource *descriptorv2.Resource `json:"resource"`
}

type ProcessResourceDigestResponse struct {
	Resource *descriptorv2.Resource `json:"resource"`
}
