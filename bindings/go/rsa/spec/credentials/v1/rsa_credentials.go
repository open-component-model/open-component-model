package v1

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// RSACredentialsType is the type name for RSA credentials.
	RSACredentialsType = "RSACredentials"
	// Version is the version of the RSA credentials type.
	Version = "v1"
)

var VersionedType = runtime.NewVersionedType(RSACredentialsType, Version)

// RSACredentials holds key material for RSA signing and/or verification.
//
// Each field has two forms: inline PEM content (PEM field) or a file path (PEMFile field).
// The inline form takes precedence when both are set.
//
// Signing requires PrivateKeyPEM or PrivateKeyPEMFile.
// For PEM-encoded signing, PublicKeyPEM or PublicKeyPEMFile should contain the certificate
// chain (leaf + intermediates) to embed in the signature.
//
// Verification of plain signatures requires PublicKeyPEM or PublicKeyPEMFile.
// If absent, the public key is derived from the private key.
// Verification of PEM-encoded signatures uses PublicKeyPEM or PublicKeyPEMFile as an
// optional trust anchor; if absent, the system root pool is used.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type RSACredentials struct {
	// +ocm:jsonschema-gen:enum=RSACredentials/v1
	// +ocm:jsonschema-gen:enum:deprecated=RSACredentials
	Type runtime.Type `json:"type"`
	// PublicKeyPEM is an inline PEM-encoded RSA public key or X.509 certificate chain.
	// For plain signature verification: the signer's public key; derived from PrivateKeyPEM if absent.
	// For PEM-encoded signing: the certificate chain (leaf + intermediates) to embed in the signature.
	// For PEM-encoded signature verification: optional trust anchor; if absent, system roots are used.
	// Takes precedence over PublicKeyPEMFile when both are set.
	PublicKeyPEM string `json:"publicKeyPEM,omitempty"`
	// PublicKeyPEMFile is a path to a PEM file containing an RSA public key or X.509 certificate chain.
	// Same semantics as PublicKeyPEM, but loaded from disk. Ignored when PublicKeyPEM is also set.
	PublicKeyPEMFile string `json:"publicKeyPEMFile,omitempty"`
	// PrivateKeyPEM is an inline PEM-encoded RSA private key (PKCS#1 or PKCS#8).
	// Required for signing; not used during verification.
	// Takes precedence over PrivateKeyPEMFile when both are set.
	PrivateKeyPEM string `json:"privateKeyPEM,omitempty"`
	// PrivateKeyPEMFile is a path to a PEM file containing an RSA private key (PKCS#1 or PKCS#8).
	// Same semantics as PrivateKeyPEM, but loaded from disk. Ignored when PrivateKeyPEM is also set.
	PrivateKeyPEMFile string `json:"privateKeyPEMFile,omitempty"`
}

// FromTyped converts runtime.Typed into RSACredentials.
// Direct conversation as well as converting from v1.DirectCredentials is supported.
// In every other case, an error will be returned.
func FromTyped(creds runtime.Typed) (*RSACredentials, error) {
	if creds == nil {
		return nil, nil
	}
	switch t := creds.(type) {
	case *RSACredentials:
		return t, nil
	case *v1.DirectCredentials:
		return FromDirectCredentials(t.Properties), nil
	case *runtime.Raw:
		RSACredentials := RSACredentials{}
		if err := Scheme.Convert(creds, &RSACredentials); err != nil {
			return nil, fmt.Errorf("error converting raw credentials to RSACredentials: %w", err)
		}
		return &RSACredentials, nil
	case *runtime.Unstructured:
		data, err := json.Marshal(t)
		if err != nil {
			return nil, fmt.Errorf("error marshalling unstructured credentials: %w", err)
		}
		RSACredentials := RSACredentials{}
		if err := json.Unmarshal(data, &RSACredentials); err != nil {
			return nil, fmt.Errorf("error converting unstructured credentials to RSACredentials: %w", err)
		}
		return &RSACredentials, nil
	}

	slog.Error("unexpected credential type, expected RSACredentials or DirectCredentials", "type", creds.GetType())
	return nil, errors.New(fmt.Sprintf("unexpected credential type: %T", creds))
}
