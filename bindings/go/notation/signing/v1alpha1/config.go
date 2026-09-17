package v1alpha1

import (
	"fmt"
	"time"

	"github.com/notaryproject/notation-go/verifier/trustpolicy"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	SignConfigType   = "NotationSigningConfiguration"
	VerifyConfigType = "NotationVerificationConfiguration"
)

var Scheme = runtime.NewScheme()

func init() {
	Scheme.MustRegisterWithAlias(&SignConfig{},
		runtime.NewUnversionedType(SignConfigType),
		runtime.NewVersionedType(SignConfigType, Version),
	)
	Scheme.MustRegisterWithAlias(&VerifyConfig{},
		runtime.NewUnversionedType(VerifyConfigType),
		runtime.NewVersionedType(VerifyConfigType, Version),
	)
}

// SignConfig defines configuration for Notation-based signing via the
// notation-go blob API.
//
// The signing key and certificate chain are NOT part of this configuration;
// they are resolved from credentials (NotationCredentials). This config only
// selects the algorithm, the signature envelope format, and an optional
// expiry.
//
// SignatureAlgorithm selects the OCM Notation algorithm version. Leave empty
// to use AlgorithmNotationDefault, which is the recommended default for new
// signatures.
//
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
type SignConfig struct {
	// Type identifies this configuration object's runtime type.
	// +ocm:jsonschema-gen:enum=NotationSigningConfiguration/v1alpha1
	Type runtime.Type `json:"type"`

	// SignatureAlgorithm selects the OCM Notation algorithm version (e.g.
	// "Notation/v1alpha1"). Optional — if empty, AlgorithmNotationDefault is
	// used. Use GetSignatureAlgorithm to read the effective value.
	SignatureAlgorithm SignatureAlgorithm `json:"signatureAlgorithm,omitempty"`

	// EnvelopeMediaType selects the signature envelope format the handler
	// produces. One of MediaTypeJWSEnvelope ("application/jose+json", the
	// default) or MediaTypeCOSEEnvelope ("application/cose"). Any other
	// non-empty value is rejected by Validate.
	EnvelopeMediaType string `json:"envelopeMediaType,omitempty"`

	// ExpiryDuration optionally sets a signature expiry, expressed as a Go
	// time.Duration string (e.g. "720h"). Empty means the signature never
	// expires. Validate rejects unparseable or negative values. Notation
	// requires a granularity of whole seconds.
	ExpiryDuration string `json:"expiryDuration,omitempty"`
}

// VerifyConfig defines configuration for Notation-based verification via the
// notation-go blob API.
//
// Trust material (the CA certificates the signer chain must terminate at) is
// resolved from credentials (NotationCredentials), not from this config. This
// config only tunes identity pinning and the verification level.
//
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
type VerifyConfig struct {
	// Type identifies this configuration object's runtime type.
	// +ocm:jsonschema-gen:enum=NotationVerificationConfiguration/v1alpha1
	Type runtime.Type `json:"type"`

	// TrustedIdentities pins the accepted signer identities as Notary Project
	// trusted-identity strings (e.g. "x509.subject: CN=acme,O=Acme"). The
	// special value ["*"] trusts any identity that chains to a trusted CA.
	// Empty defaults to ["*"]: chain-to-trusted-CA is always enforced, while
	// identity pinning is opt-in.
	TrustedIdentities []string `json:"trustedIdentities,omitempty"`

	// VerificationLevel selects the Notary Project verification level: one of
	// "strict" (the default), "permissive", or "audit". Validate rejects
	// unknown values.
	VerificationLevel string `json:"verificationLevel,omitempty"`
}

// Validate checks that SignConfig fields are well-formed.
func (c *SignConfig) Validate() error {
	switch c.SignatureAlgorithm {
	case "", AlgorithmNotationV1Alpha1:
	default:
		return fmt.Errorf("signatureAlgorithm: %w: %q", ErrUnknownAlgorithm, c.SignatureAlgorithm)
	}
	switch c.EnvelopeMediaType {
	case "", MediaTypeJWSEnvelope, MediaTypeCOSEEnvelope:
	default:
		return fmt.Errorf("envelopeMediaType: unsupported value %q (want %q or %q)", c.EnvelopeMediaType, MediaTypeJWSEnvelope, MediaTypeCOSEEnvelope)
	}
	if c.ExpiryDuration != "" {
		d, err := time.ParseDuration(c.ExpiryDuration)
		if err != nil {
			return fmt.Errorf("expiryDuration: %w", err)
		}
		if d < 0 {
			return fmt.Errorf("expiryDuration: must not be negative, got %q", c.ExpiryDuration)
		}
	}
	return nil
}

// GetSignatureAlgorithm returns the canonical signing algorithm. Empty resolves
// to AlgorithmNotationDefault. Validate must have been called and returned nil
// before this method is read.
func (c *SignConfig) GetSignatureAlgorithm() SignatureAlgorithm {
	if c.SignatureAlgorithm == "" {
		return AlgorithmNotationDefault
	}
	return c.SignatureAlgorithm
}

// GetEnvelopeMediaType returns the effective signature envelope media type,
// defaulting to MediaTypeJWSEnvelope when unset. Validate must have been called
// and returned nil before this method is read.
func (c *SignConfig) GetEnvelopeMediaType() string {
	if c.EnvelopeMediaType == "" {
		return MediaTypeJWSEnvelope
	}
	return c.EnvelopeMediaType
}

// GetExpiryDuration returns the parsed expiry duration, or zero when unset.
// Validate must have been called and returned nil before this method is read.
func (c *SignConfig) GetExpiryDuration() time.Duration {
	if c.ExpiryDuration == "" {
		return 0
	}
	d, _ := time.ParseDuration(c.ExpiryDuration)
	return d
}

// Validate checks that VerifyConfig fields are well-formed.
func (c *VerifyConfig) Validate() error {
	switch c.VerificationLevel {
	case "", trustpolicy.LevelStrict.Name, trustpolicy.LevelPermissive.Name, trustpolicy.LevelAudit.Name:
	default:
		return fmt.Errorf("verificationLevel: unsupported value %q (want %q, %q, or %q)",
			c.VerificationLevel, trustpolicy.LevelStrict.Name, trustpolicy.LevelPermissive.Name, trustpolicy.LevelAudit.Name)
	}
	return nil
}

// GetVerificationLevel returns the effective Notary Project verification level
// name, defaulting to strict when unset. Validate must have been called and
// returned nil before this method is read.
func (c *VerifyConfig) GetVerificationLevel() string {
	if c.VerificationLevel == "" {
		return trustpolicy.LevelStrict.Name
	}
	return c.VerificationLevel
}

// GetTrustedIdentities returns the effective trusted-identity filters,
// defaulting to ["*"] (trust any identity chaining to a trusted CA) when unset.
func (c *VerifyConfig) GetTrustedIdentities() []string {
	if len(c.TrustedIdentities) == 0 {
		return []string{"*"}
	}
	return c.TrustedIdentities
}
