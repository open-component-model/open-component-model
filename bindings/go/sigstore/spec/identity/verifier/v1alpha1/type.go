package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// SigstoreVerifierIdentityType is the type name for Sigstore verifier consumer identities.
	SigstoreVerifierIdentityType = "SigstoreVerifier"
	// V1Alpha1Version is the legacy v1alpha1 version, kept for backward compatibility.
	V1Alpha1Version = "v1alpha1"
	// Version is the current version of the SigstoreVerifierIdentity type.
	Version = V1Alpha1Version
)

var Type = runtime.NewUnversionedType(SigstoreVerifierIdentityType)

var VersionedType = runtime.NewVersionedType(SigstoreVerifierIdentityType, Version)

const (
	// IdentityAttributeAlgorithm is the key for the signing algorithm in a credential consumer identity map.
	IdentityAttributeAlgorithm = "algorithm"
	// IdentityAttributeSignature is the key for the signature name in a credential consumer identity map.
	IdentityAttributeSignature = "signature"
)

// SigstoreVerifierIdentity is the typed consumer identity for Sigstore verification handlers.
//
// The credential system matches this identity against configured credentials to resolve
// the [SigstoreCredentials] used during cosign verify-blob. Both fields are optional
// filters: omitting a field matches any value for that attribute.
//
// Algorithm and Signature correspond to the identity attributes [IdentityAttributeAlgorithm]
// and [IdentityAttributeSignature] in the flat runtime.Identity map produced by
// GetVerifyingCredentialConsumerIdentity.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type SigstoreVerifierIdentity struct {
	// +ocm:jsonschema-gen:enum=SigstoreVerifier/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=SigstoreVerifier
	Type runtime.Type `json:"type"`
	// Algorithm restricts this identity to a specific signing algorithm.
	// For Sigstore keyless signing the value is "sigstore" (v1alpha1.AlgorithmSigstore).
	// Omit to match all algorithms.
	Algorithm string `json:"algorithm,omitempty"`
	// Signature restricts this identity to a specific named signature within a component version.
	// The value must match the signature name passed to GetVerifyingCredentialConsumerIdentity
	// (i.e. the Name field of the descruntime.Signature being verified).
	// Omit to match all signature names.
	Signature string `json:"signature,omitempty"`
}
