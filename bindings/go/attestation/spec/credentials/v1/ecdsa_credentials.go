package v1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// ECDSACredentialsType is the type name for ECDSA credentials.
	ECDSACredentialsType = "ECDSACredentials"
	// Version is the version of the ECDSA credentials type.
	Version = "v1"
)

// VersionedType is the versioned runtime type for ECDSA credentials.
var VersionedType = runtime.NewVersionedType(ECDSACredentialsType, Version)

// ECDSACredentials holds key material for ECDSA P-256 attestation signing.
//
// Each field has two forms: inline PEM content (PEM field) or a file path
// (PEMFile field). The inline form takes precedence when both are set.
//
// Signing requires PrivateKeyPEM or PrivateKeyPEMFile. PublicKeyPEM or
// PublicKeyPEMFile is optional; if absent, the public key is derived from the
// private key.
type ECDSACredentials struct {
	Type runtime.Type `json:"type"`
	// PrivateKeyPEM is an inline PEM-encoded ECDSA P-256 private key (PKCS#8 or SEC1). Required for signing.
	PrivateKeyPEM string `json:"privateKeyPEM,omitempty"`
	// PrivateKeyPEMFile is a path to a PEM file containing an ECDSA P-256 private key. Ignored when PrivateKeyPEM is set.
	PrivateKeyPEMFile string `json:"privateKeyPEMFile,omitempty"`
	// PublicKeyPEM is an inline PEM-encoded ECDSA P-256 public key (PKIX). Optional; derived from the private key if absent.
	PublicKeyPEM string `json:"publicKeyPEM,omitempty"`
	// PublicKeyPEMFile is a path to a PEM file containing an ECDSA P-256 public key. Ignored when PublicKeyPEM is set.
	PublicKeyPEMFile string `json:"publicKeyPEMFile,omitempty"`
}

// GetType returns the runtime type of the credentials.
func (in *ECDSACredentials) GetType() runtime.Type { return in.Type }

// SetType sets the runtime type of the credentials.
func (in *ECDSACredentials) SetType(t runtime.Type) { in.Type = t }

// DeepCopyInto copies the receiver into out.
func (in *ECDSACredentials) DeepCopyInto(out *ECDSACredentials) {
	*out = *in
	out.Type = in.Type
}

// DeepCopy returns a deep copy of the credentials.
func (in *ECDSACredentials) DeepCopy() *ECDSACredentials {
	if in == nil {
		return nil
	}
	out := new(ECDSACredentials)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyTyped returns a deep copy as a runtime.Typed.
func (in *ECDSACredentials) DeepCopyTyped() runtime.Typed {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}
