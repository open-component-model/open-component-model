package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
type DirectCredentials struct {
	Type       runtime.Type      `json:"type"`
	Properties map[string]string `json:"properties"`
}
