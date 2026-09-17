package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	NotationIdentityType = "Notation"
	Version              = "v1"
)

// Type is the unversioned consumer identity type for Notation signing.
var Type = runtime.NewUnversionedType(NotationIdentityType)

// VersionedType is the versioned consumer identity type.
var VersionedType = runtime.NewVersionedType(NotationIdentityType, Version)

// Identity attribute keys for Notation signing credentials.
const (
	IdentityAttributeSignature = "signature"
)

// NotationIdentity is the typed consumer identity for Notation signing
// handlers. It is symmetric: the same identity type is used for both signing
// and verification.
//
// The credential system matches this identity against configured credentials
// to resolve the [NotationCredentials] used during sign/verify. All fields are
// optional filters: omitting a field matches any value for that attribute.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type NotationIdentity struct {
	// +ocm:jsonschema-gen:enum=Notation/v1
	// +ocm:jsonschema-gen:enum:deprecated=Notation
	Type runtime.Type `json:"type"`
	// Signature restricts this identity to a specific named signature within a
	// component version. Omit to match all signature names.
	Signature string `json:"signature,omitempty"`
}
