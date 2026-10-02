package v1alpha1

import (
	"strings"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const ConfigType = "GPGSigningConfiguration"

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&Config{},
		runtime.NewUnversionedType(ConfigType),
		runtime.NewVersionedType(ConfigType, Version),
	)
}

// Config defines configuration for OpenPGP (GPG) signing and verification.
//
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
type Config struct {
	// Type identifies this configuration object's runtime type.
	// +ocm:jsonschema-gen:enum=GPGSigningConfiguration/v1alpha1
	// +ocm:jsonschema-gen:enum:deprecated=GPGSigningConfiguration
	Type runtime.Type `json:"type"`

	// HashAlgorithm selects the hash function applied to the digest bytes before signing.
	// Defaults to SHA-256 when empty.
	// Supported values: SHA-256, SHA-384, SHA-512.
	HashAlgorithm HashAlgorithm `json:"hashAlgorithm,omitempty"`

	// KeyFingerprint pins which key in the keyring to use when signing or verifying.
	// When empty, signing uses the first secret key in the key material (gpg's default key with
	// KeySource keyring), and verification accepts a signature by any key in the public key material.
	// Accepts a full 40-hex-character v4 fingerprint or a 16-hex-character long key ID, optionally
	// with a 0x prefix and with spaces as printed by gpg --fingerprint.
	KeyFingerprint string `json:"keyFingerprint,omitempty"`

	// KeySource selects where the keys for signing and verification come from.
	// Defaults to credentials when omitted.
	// Supported values: credentials, keyring.
	KeySource KeySource `json:"keySource,omitempty"`
}

// KeySource names where the GPG handler takes its keys from.
type KeySource string

const (
	// KeySourceCredentials takes the key material from the GPG credentials and runs gpg
	// in a temporary, isolated GnuPG home directory per operation.
	KeySourceCredentials KeySource = "credentials"
	// KeySourceKeyring takes the keys from the user's GnuPG keyring ($GNUPGHOME, or ~/.gnupg)
	// and the running gpg-agent. This enables hardware tokens and the agent's passphrase cache.
	// Key material in the credentials is rejected; a passphrase is still used if set.
	// Verification requires KeyFingerprint to be a full fingerprint, so that only the pinned key
	// is accepted out of all keys in the keyring.
	KeySourceKeyring KeySource = "keyring"
)

// GetKeySource returns the configured key source, defaulting to credentials.
func (c *Config) GetKeySource() KeySource {
	if c == nil || c.KeySource == "" {
		return KeySourceCredentials
	}
	return c.KeySource
}

// GetHashAlgorithm returns the configured hash algorithm, defaulting to SHA-256.
func (c *Config) GetHashAlgorithm() HashAlgorithm {
	if c == nil || c.HashAlgorithm == "" {
		return HashAlgorithmSHA256
	}
	return c.HashAlgorithm
}

// GetKeyFingerprint returns the configured key fingerprint (may be empty) without a 0x prefix
// and without whitespace, so that the spaced output of gpg --fingerprint can be pasted as is.
func (c *Config) GetKeyFingerprint() string {
	if c == nil {
		return ""
	}
	fpr := strings.Join(strings.Fields(c.KeyFingerprint), "")
	if len(fpr) > 2 && (fpr[:2] == "0x" || fpr[:2] == "0X") {
		fpr = fpr[2:]
	}
	return fpr
}
