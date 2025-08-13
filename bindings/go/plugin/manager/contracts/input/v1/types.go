package v1

import (
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/plugin/manager/types"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// GetIdentityRequest contains a type for which an identity get be provided during a GetIdentity call.
type GetIdentityRequest[T runtime.Typed] struct {
	Typ T `json:"type"`
}

// GetIdentityResponse contains identity information provided by the plugin during a GetIdentity call.
type GetIdentityResponse struct {
	Identity map[string]string `json:"identity"`
}

// ProcessResourceInputRequest contains the resource to process an input for. This resource is of a specific version
// because we need to be able to serialize it.
type ProcessResourceInputRequest struct {
	Resource *descriptorv2.Resource `json:"resource"`
}

// ProcessResourceInputResponse contains the resource for which an input was processed. This resource is of a specific version
// because we need to be able to serialize it.
type ProcessResourceInputResponse struct {
	Resource *descriptorv2.Resource `json:"resource"`
	Location *types.Location        `json:"location"`
}

// ProcessSourceInputRequest contains the source to process an input for. This source is of a specific version
// because we need to be able to serialize it.
type ProcessSourceInputRequest struct {
	Source *descriptorv2.Source `json:"source"`
}

// ProcessSourceInputResponse contains the source that was an input was processed. This source is of a specific version
// because we need to be able to serialize it.
type ProcessSourceInputResponse struct {
	Source   *descriptorv2.Source `json:"source"`
	Location *types.Location      `json:"location"`
}
