package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// NotationCredentialsType is the type name for Notation credentials.
	NotationCredentialsType = "NotationCredentials"
	// Version is the version of the Notation credentials type.
	Version = "v1"
)

var VersionedType = runtime.NewVersionedType(NotationCredentialsType, Version)

// NotationCredentials holds key and trust material for Notation signing and/or
// verification.
//
// Each field has two forms: inline PEM content (PEM field) or a file path
// (PEMFile field). The inline form takes precedence when both are set.
//
// Signing requires PrivateKeyPEM or PrivateKeyPEMFile AND a certificate chain
// (CertificateChainPEM or CertificateChainPEMFile): notation-go's generic
// signer needs both a private key and the matching X.509 chain.
//
// Verification requires TrustedCACertificatesPEM or TrustedCACertificatesPEMFile:
// the CA certificate(s) the signer's chain must terminate at. There is no
// system-root fallback for Notation verification; the trust anchor is always
// explicit.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type NotationCredentials struct {
	// +ocm:jsonschema-gen:enum=NotationCredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=NotationCredentials
	Type runtime.Type `json:"type"`
	// PrivateKeyPEM is an inline PEM-encoded private key (PKCS#1 or PKCS#8).
	// Required for signing; not used during verification.
	// Takes precedence over PrivateKeyPEMFile when both are set.
	PrivateKeyPEM string `json:"privateKeyPEM,omitempty"`
	// PrivateKeyPEMFile is a path to a PEM file containing a private key
	// (PKCS#1 or PKCS#8). Same semantics as PrivateKeyPEM, but loaded from
	// disk. Ignored when PrivateKeyPEM is also set.
	PrivateKeyPEMFile string `json:"privateKeyPEMFile,omitempty"`
	// CertificateChainPEM is an inline PEM-encoded X.509 certificate chain
	// (signer leaf followed by any intermediates). Embedded into the signature
	// envelope during signing. Required for signing.
	// Takes precedence over CertificateChainPEMFile when both are set.
	CertificateChainPEM string `json:"certificateChainPEM,omitempty"`
	// CertificateChainPEMFile is a path to a PEM file containing the signer's
	// X.509 certificate chain. Same semantics as CertificateChainPEM, but
	// loaded from disk. Ignored when CertificateChainPEM is also set.
	CertificateChainPEMFile string `json:"certificateChainPEMFile,omitempty"`
	// TrustedCACertificatesPEM is an inline PEM bundle of the CA certificate(s)
	// the signer chain must terminate at. Populates the verifier trust store.
	// Required for verification.
	// Takes precedence over TrustedCACertificatesPEMFile when both are set.
	TrustedCACertificatesPEM string `json:"trustedCACertificatesPEM,omitempty"`
	// TrustedCACertificatesPEMFile is a path to a PEM bundle of trusted CA
	// certificate(s). Same semantics as TrustedCACertificatesPEM, but loaded
	// from disk. Ignored when TrustedCACertificatesPEM is also set.
	TrustedCACertificatesPEMFile string `json:"trustedCACertificatesPEMFile,omitempty"`
}
