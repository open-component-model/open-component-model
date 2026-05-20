package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	TrustedRootType = "TrustedRoot"
	Version         = "v1"
)

var TrustedRootVersionedType = runtime.NewVersionedType(TrustedRootType, Version)

// TrustedRoot represents typed credentials for Sigstore verification trust material.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TrustedRoot struct {
	// +ocm:jsonschema-gen:enum=TrustedRoot/v1
	// +ocm:jsonschema-gen:enum:deprecated=TrustedRoot
	Type                runtime.Type `json:"type"`
	TrustedRootJSON     string       `json:"trustedRootJSON,omitempty"`
	TrustedRootJSONFile string       `json:"trustedRootJSONFile,omitempty"`
}
