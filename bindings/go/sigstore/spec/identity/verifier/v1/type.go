package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	SigstoreVerifierIdentityType = "SigstoreVerifier"
	Version                      = "v1"
	V1Alpha1Version              = "v1alpha1"
)

var Type = runtime.NewUnversionedType(SigstoreVerifierIdentityType)

var VersionedType = runtime.NewVersionedType(SigstoreVerifierIdentityType, Version)

var V1Alpha1Type = runtime.NewVersionedType(SigstoreVerifierIdentityType, V1Alpha1Version)

const (
	IdentityAttributeAlgorithm = "algorithm"
	IdentityAttributeSignature = "signature"
)

// SigstoreVerifierIdentity is the typed consumer identity for Sigstore verification handlers.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type SigstoreVerifierIdentity struct {
	// +ocm:jsonschema-gen:enum=SigstoreVerifier/v1
	// +ocm:jsonschema-gen:enum:deprecated=SigstoreVerifier/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=SigstoreVerifier
	Type      runtime.Type `json:"type"`
	Algorithm string       `json:"algorithm,omitempty"`
	Signature string       `json:"signature,omitempty"`
}
