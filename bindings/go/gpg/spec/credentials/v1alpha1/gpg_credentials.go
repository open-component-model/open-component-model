package v1alpha1

import (
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// GPGCredentialsType is the type name for GPG credentials.
	GPGCredentialsType = "GPGCredentials"
	// Version is the version of the GPG credentials type.
	Version = "v1alpha1"
)

// GPGCredentials represents typed credentials for GPG signing and verification.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type GPGCredentials struct {
	// +ocm:jsonschema-gen:enum=GPGCredentials/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=GPGCredentials
	Type runtime.Type `json:"type"`
	// PrivateKeyPGP is an inline ASCII-armored OpenPGP private key (or keyring).
	// Required for signing; not used during verification.
	// Takes precedence over PrivateKeyPGPFile when both are set.
	PrivateKeyPGP string `json:"privateKeyPGP,omitempty"`
	// PrivateKeyPGPFile is a path to a file containing an ASCII-armored OpenPGP private key.
	// Same semantics as PrivateKeyPGP, but loaded from disk. Ignored when PrivateKeyPGP is also set.
	PrivateKeyPGPFile string `json:"privateKeyPGPFile,omitempty"`
	// PublicKeyPGP is an inline ASCII-armored OpenPGP public key (or keyring) for verification.
	// If absent, the public key is derived from PrivateKeyPGP for verification.
	// Takes precedence over PublicKeyPGPFile when both are set.
	PublicKeyPGP string `json:"publicKeyPGP,omitempty"`
	// PublicKeyPGPFile is a path to a file containing an ASCII-armored OpenPGP public key.
	// Same semantics as PublicKeyPGP, but loaded from disk. Ignored when PublicKeyPGP is also set.
	PublicKeyPGPFile string `json:"publicKeyPGPFile,omitempty"`
	// KeyringFingerprint loads the keys from the user's GnuPG keyring instead of key material: the full fingerprint
	// (40 or 64 hex characters, spaces and a 0x prefix allowed) of the key to use. OCM runs the GnuPG gpg binary (>= 2.2.0,
	// on PATH) only to export this key: gpg --export for verification, gpg --export-secret-keys for signing. All
	// cryptography runs in OCM. Mutually exclusive with PrivateKeyPGP, PrivateKeyPGPFile, PublicKeyPGP and PublicKeyPGPFile.
	// A passphrase-protected key needs Passphrase. Keys on hardware tokens are not supported.
	KeyringFingerprint string `json:"keyringFingerprint,omitempty"`
	// KeyringHome is the GnuPG home directory of the keyring (gpg --homedir). Defaults to gpg's default ($GNUPGHOME or ~/.gnupg).
	KeyringHome string `json:"keyringHome,omitempty"`
	// Passphrase decrypts a passphrase-protected private key, also when exporting it from the keyring.
	// Required when the private key is encrypted; omit for unprotected keys.
	Passphrase string `json:"passphrase,omitempty"`
}

// MustRegisterCredentialType registers GPGCredentials/v1alpha1 in the given scheme.
func MustRegisterCredentialType(scheme *runtime.Scheme) {
	scheme.MustRegisterWithAlias(&GPGCredentials{},
		runtime.NewVersionedType(GPGCredentialsType, Version),
		runtime.NewUnversionedType(GPGCredentialsType),
	)
}
