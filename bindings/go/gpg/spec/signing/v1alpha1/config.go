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

	// HashAlgorithm selects the hash function of the signature over the digest bytes.
	// Supported values: SHA-256, SHA-384, SHA-512. When empty, SHA-256, unless the signing key needs a longer hash:
	// SHA-384 for ECDSA P-384 and Brainpool P-384 keys, SHA-512 for ECDSA P-521, Brainpool P-512 and Ed448 keys.
	// A hash shorter than the signing key needs is an error.
	HashAlgorithm HashAlgorithm `json:"hashAlgorithm,omitempty"`

	// KeyFingerprint pins which key to use when signing or verifying.
	// When empty, signing uses the first secret key in the private key material (or the keyringFingerprint
	// key of the credentials), and verification accepts a signature by any key in the public key material.
	// Accepts a full fingerprint (40 hex characters for v4 keys, 64 for v6 keys) of the primary key or a
	// subkey, or a 16-hex-character long key ID, optionally with a 0x prefix and with spaces.
	KeyFingerprint string `json:"keyFingerprint,omitempty"`
}

// GetKeyFingerprint returns the configured key fingerprint (may be empty), normalized by NormalizeFingerprint.
func (c *Config) GetKeyFingerprint() string {
	if c == nil {
		return ""
	}
	return NormalizeFingerprint(c.KeyFingerprint)
}

// NormalizeFingerprint strips all whitespace and a leading 0x or 0X from a fingerprint or key ID,
// so that the spaced output of gpg --fingerprint can be pasted as is.
func NormalizeFingerprint(s string) string {
	fpr := strings.Join(strings.Fields(s), "")
	if len(fpr) > 2 && (fpr[:2] == "0x" || fpr[:2] == "0X") {
		fpr = fpr[2:]
	}
	return fpr
}
