package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	GPGIdentityType = "GPG"
	Version         = "v1alpha1"
)

// Type is the unversioned consumer identity type for GPG signing (backward compat).
var Type = runtime.NewUnversionedType(GPGIdentityType)

// V1Alpha1Type is the versioned consumer identity type.
var V1Alpha1Type = runtime.NewVersionedType(GPGIdentityType, Version)

// Identity attribute keys for GPG signing credentials.
const (
	IdentityAttributeSignature = "signature"
)

// GPGIdentity is the typed consumer identity for GPG signing handlers.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GPGIdentity struct {
	// +ocm:jsonschema-gen:enum=GPG/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=GPG
	Type      runtime.Type `json:"type"`
	Signature string       `json:"signature,omitempty"`
}
