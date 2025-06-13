package v1

import (
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
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

// ProcessResourceDigestRequest contains a descriptorv2.Resource for which to process a digest request for. Note that
// this request needs to be serializable, thus contains a specific version of descriptor.Resource.
type ProcessResourceDigestRequest struct {
	Resource *descriptorv2.Resource `json:"resource"`
}

// ProcessResourceDigestResponse contains a descriptorv2.Resource as a response for a digest call. Note that
// this request needs to be serializable, thus contains a specific version of descriptor.Resource.
type ProcessResourceDigestResponse struct {
	Resource *descriptorv2.Resource `json:"resource"`
}
