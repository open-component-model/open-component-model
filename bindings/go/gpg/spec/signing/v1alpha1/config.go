package v1alpha1

import (
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
	// When empty the first available key is used.
	// Accepts a full 40-hex-character v4 fingerprint or a 16-hex-character long key ID.
	KeyFingerprint string `json:"keyFingerprint,omitempty"`
}

// GetHashAlgorithm returns the configured hash algorithm, defaulting to SHA-256.
func (c *Config) GetHashAlgorithm() HashAlgorithm {
	if c == nil || c.HashAlgorithm == "" {
		return HashAlgorithmSHA256
	}
	return c.HashAlgorithm
}

// GetKeyFingerprint returns the configured key fingerprint (may be empty).
func (c *Config) GetKeyFingerprint() string {
	if c == nil {
		return ""
	}
	return c.KeyFingerprint
}
