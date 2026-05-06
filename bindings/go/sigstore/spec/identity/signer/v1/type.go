package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	SigstoreSignerIdentityType = "SigstoreSigner"
	Version                    = "v1"
	V1Alpha1Version            = "v1alpha1"
)

var Type = runtime.NewUnversionedType(SigstoreSignerIdentityType)

var VersionedType = runtime.NewVersionedType(SigstoreSignerIdentityType, Version)

var V1Alpha1Type = runtime.NewVersionedType(SigstoreSignerIdentityType, V1Alpha1Version)

const (
	IdentityAttributeAlgorithm = "algorithm"
	IdentityAttributeSignature = "signature"
)

// SigstoreSignerIdentity is the typed consumer identity for Sigstore signing handlers.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type SigstoreSignerIdentity struct {
	// +ocm:jsonschema-gen:enum=SigstoreSigner/v1
	// +ocm:jsonschema-gen:enum:deprecated=SigstoreSigner/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=SigstoreSigner
	Type      runtime.Type `json:"type"`
	Algorithm string       `json:"algorithm,omitempty"`
	Signature string       `json:"signature,omitempty"`
}
