package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	RSAIdentityType = "RSA"
	Version         = "v1"
	// V1Alpha1Version is the legacy version for backward compatibility.
	V1Alpha1Version = "v1alpha1"
)

// Type is the unversioned consumer identity type for RSA signing (backward compat).
var Type = runtime.NewUnversionedType(RSAIdentityType)

// VersionedType is the versioned consumer identity type.
var VersionedType = runtime.NewVersionedType(RSAIdentityType, Version)

// V1Alpha1Type is the legacy versioned identity type for backward compatibility.
var V1Alpha1Type = runtime.NewVersionedType(RSAIdentityType, V1Alpha1Version)

// Identity attribute keys for RSA signing credentials.
const (
	IdentityAttributeAlgorithm = "algorithm"
	IdentityAttributeSignature = "signature"
)

// RSAIdentity is the typed consumer identity for RSA signing handlers.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type RSAIdentity struct {
	// +ocm:jsonschema-gen:enum=RSA/v1
	// +ocm:jsonschema-gen:enum:deprecated=RSA/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=RSA
	Type      runtime.Type `json:"type"`
	Algorithm string       `json:"algorithm,omitempty"`
	Signature string       `json:"signature,omitempty"`
}
