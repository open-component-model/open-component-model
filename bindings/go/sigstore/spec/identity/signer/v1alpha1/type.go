package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// SigstoreSignerIdentityType is the type name for Sigstore signer consumer identities.
	SigstoreSignerIdentityType = "SigstoreSigner"
	// V1Alpha1Version is the legacy v1alpha1 version, kept for backward compatibility.
	V1Alpha1Version = "v1alpha1"
	// Version is the current version of the SigstoreSignerIdentityType type.
	Version = V1Alpha1Version
)

var Type = runtime.NewUnversionedType(SigstoreSignerIdentityType)

var VersionedType = runtime.NewVersionedType(SigstoreSignerIdentityType, Version)

const (
	// IdentityAttributeAlgorithm is the key for the signing algorithm in a credential consumer identity map.
	IdentityAttributeAlgorithm = "algorithm"
	// IdentityAttributeSignature is the key for the signature name in a credential consumer identity map.
	IdentityAttributeSignature = "signature"
	// IdentityAttributeIssuer is the key for the OIDC issuer URL in a signer credential consumer identity map.
	// Populated from SignConfig.Issuer; used to scope credentials to a specific Fulcio/issuer endpoint.
	IdentityAttributeIssuer = "issuer"
	// IdentityAttributeClientID is the key for the OIDC client ID in a signer credential consumer identity map.
	// Populated from SignConfig.ClientID; used to scope credentials to a specific OIDC client.
	IdentityAttributeClientID = "clientID"
)

// SigstoreSignerIdentity is the typed consumer identity for Sigstore signing handlers.
//
// The credential system matches this identity against configured credentials to resolve
// the [SigstoreCredentials] used during cosign sign-blob. All fields are optional
// filters: omitting a field matches any value for that attribute.
//
// Each field corresponds to an identity attribute constant ([IdentityAttributeAlgorithm],
// [IdentityAttributeSignature], [IdentityAttributeIssuer], [IdentityAttributeClientID])
// in the flat runtime.Identity map produced by GetSigningCredentialConsumerIdentity.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type SigstoreSignerIdentity struct {
	// +ocm:jsonschema-gen:enum=SigstoreSigner/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=SigstoreSigner
	Type runtime.Type `json:"type"`
	// Algorithm restricts this identity to a specific signing algorithm.
	// For Sigstore keyless signing the value is "sigstore" (v1alpha1.AlgorithmSigstore).
	// Omit to match all algorithms.
	Algorithm string `json:"algorithm,omitempty"`
	// Signature restricts this identity to a specific named signature within a component version.
	// The value must match the signature name passed to GetSigningCredentialConsumerIdentity.
	// Omit to match all signature names.
	Signature string `json:"signature,omitempty"`
	// Issuer restricts this identity to a specific OIDC issuer URL (e.g. the Fulcio endpoint).
	// Populated from SignConfig.Issuer. Omit to match any issuer.
	Issuer string `json:"issuer,omitempty"`
	// ClientID restricts this identity to a specific OIDC client ID.
	// Populated from SignConfig.ClientID. Omit to match any client ID.
	ClientID string `json:"clientID,omitempty"`
}
