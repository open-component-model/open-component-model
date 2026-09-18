package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// TSACredentialsType is the type name for RFC 3161 Timestamping Authority credentials.
	TSACredentialsType = "TSACredentials"
	// Version is the version of the TSACredentials type.
	Version = "v1alpha1"
)

// VersionedType is the canonical versioned [runtime.Type] for TSACredentials.
var VersionedType = runtime.NewVersionedType(TSACredentialsType, Version)

// TSACredentials carries the trust material used by the verification path to
// validate the PKCS#7 signature chain of an RFC 3161 timestamp token.
//
// It is consumed by the verifier when a signature carries a timestamp: the root
// certificates supplied here are the exclusive trust anchor for the TSA's token.
// They are never taken from the signature itself, so a signer cannot assert its
// own TSA trust anchor. Without a matching TSACredentials entry, timestamp
// verification degrades to structural-only mode (see [ADR 0030]).
//
// The root certificates have two forms: inline PEM content (RootCertsPEM) or a
// file path (RootCertsPEMFile). The inline form takes precedence when both are
// set. Both forms accept a PEM bundle containing one or more CERTIFICATE blocks.
//
// This credential is consumed only by the verification path; the signing path
// selects the TSA via the --tsa / --tsa-url flags and does not read credentials.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
//
// [ADR 0030]: https://github.com/open-component-model/open-component-model/blob/main/docs/adr/0030_rfc3161_timestamping.md
type TSACredentials struct {
	// +ocm:jsonschema-gen:enum=TSACredentials/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=TSACredentials
	Type runtime.Type `json:"type"`
	// RootCertsPEM is an inline PEM bundle of root CA certificates used to verify
	// the TSA's PKCS#7 timestamp token chain. Takes precedence over RootCertsPEMFile
	// when both are set.
	RootCertsPEM string `json:"rootCertsPEM,omitempty"`
	// RootCertsPEMFile is a path to a PEM file containing root CA certificates used
	// to verify the TSA's PKCS#7 timestamp token chain. Same semantics as
	// RootCertsPEM, but loaded from disk. Ignored when RootCertsPEM is also set.
	RootCertsPEMFile string `json:"rootCertsPEMFile,omitempty"`
}

// MustRegisterCredentialType registers TSACredentials/v1alpha1 (and its
// unversioned alias) in the given scheme. The credential graph uses this to
// deserialize TSA/v1alpha1 consumer credentials into the typed struct instead
// of falling back to DirectCredentials.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&TSACredentials{},
		VersionedType,
		runtime.NewUnversionedType(TSACredentialsType),
	)
}
