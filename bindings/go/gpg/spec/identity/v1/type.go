package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	GPGIdentityType = "GPG"
	Version         = "v1"
	// V1Alpha1Version is the legacy version for backward compatibility.
	V1Alpha1Version = "v1alpha1"
)

// Type is the unversioned consumer identity type for GPG signing (backward compat).
var Type = runtime.NewUnversionedType(GPGIdentityType)

// VersionedType is the versioned consumer identity type.
var VersionedType = runtime.NewVersionedType(GPGIdentityType, Version)

// V1Alpha1Type is the legacy versioned identity type for backward compatibility.
var V1Alpha1Type = runtime.NewVersionedType(GPGIdentityType, V1Alpha1Version)

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
	// +ocm:jsonschema-gen:enum=GPG/v1
	// +ocm:jsonschema-gen:enum:deprecated=GPG/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=GPG
	Type      runtime.Type `json:"type"`
	Signature string       `json:"signature,omitempty"`
}
