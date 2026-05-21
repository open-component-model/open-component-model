package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// TrustedRootType is the type name for Sigstore trusted-root credentials.
	TrustedRootType = "TrustedRoot"
	// Version is the version of the TrustedRoot type.
	Version = "v1alpha1"
)

// VersionedType is the canonical versioned [runtime.Type] for TrustedRoot credentials.
var VersionedType = runtime.NewVersionedType(TrustedRootType, Version)

// TrustedRoot carries Sigstore trust material used by the verification path to validate
// Fulcio certificate chains, Rekor inclusion proofs, and TSA timestamps. It overrides
// the default public-good Sigstore TUF root and is required when verifying signatures
// produced against private Sigstore infrastructure (see VerifyConfig.PrivateInfrastructure).
//
// Provide either TrustedRootJSON (inline) or TrustedRootJSONFile (path); TrustedRootJSON
// takes precedence when both are set. TrustedRootJSONFile must be an absolute, canonical
// path (no .. segments).
//
// This credential is consumed by the sigstore signing handler's Verify path. It is not
// consumed by the Sign path; signing uses [OIDCIdentityToken] instead.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type TrustedRoot struct {
	// +ocm:jsonschema-gen:enum=TrustedRoot/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=TrustedRoot
	Type runtime.Type `json:"type"`
	// TrustedRootJSON is an inline JSON document conforming to the Sigstore TrustedRoot schema.
	// Overrides the default public-good TUF root, enabling verification against private Sigstore
	// infrastructure (required when VerifyConfig.PrivateInfrastructure is true).
	// Written to a temp file and passed to cosign as --trusted-root.
	// Takes precedence over TrustedRootJSONFile when both are set.
	TrustedRootJSON string `json:"trustedRootJSON,omitempty"`
	// TrustedRootJSONFile is a path to a JSON file conforming to the Sigstore TrustedRoot schema.
	// Same semantics as TrustedRootJSON, but loaded from disk; passed directly to cosign as
	// --trusted-root without an intermediate temp file. Ignored when TrustedRootJSON is also set.
	// Must be an absolute, canonical path (no .. segments).
	TrustedRootJSONFile string `json:"trustedRootJSONFile,omitempty"`
}
