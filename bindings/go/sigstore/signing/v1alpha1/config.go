package v1alpha1

import (
	"fmt"
	"net/url"
	"regexp"

	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	SignConfigType   = "SigstoreSigningConfiguration"
	VerifyConfigType = "SigstoreVerificationConfiguration"
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

// SignConfig defines configuration for Sigstore-based keyless signing via the cosign CLI.
//
// Endpoint configuration:
//  1. SigningConfig — cosign reads a local signing_config.json for endpoint
//     discovery (Fulcio, Rekor, TSA). Create one with `cosign signing-config create`.
//  2. Neither set — cosign's default fetches the signing config from the
//     public-good Sigstore TUF repository.
//
// When an OIDC token is available in credentials, it is forwarded to cosign
// via the SIGSTORE_ID_TOKEN environment variable. A token is required;
// the handler returns an error if no token credential is resolved.
//
// Trust material (trusted root) is resolved from credentials, not from this
// config. See the handler package for resolution order.
//
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
type SignConfig struct {
	// Type identifies this configuration object's runtime type.
	// +ocm:jsonschema-gen:enum=SigstoreSigningConfiguration/v1alpha1
	Type runtime.Type `json:"type"`

	// SigningConfig is a filesystem path to a cosign signing configuration file
	// (conventionally named signing_config.json).
	// When set, cosign discovers all service endpoints (Fulcio, Rekor, TSA) from
	// this file instead of TUF auto-discovery.
	// Maps to cosign --signing-config.
	SigningConfig string `json:"signingConfig,omitempty"`
}

// VerifyConfig defines configuration for Sigstore-based keyless verification via the cosign CLI.
//
// For keyless (Sigstore) verification, identity constraints are REQUIRED: you must set either
// CertificateOIDCIssuer (or CertificateOIDCIssuerRegexp) AND CertificateIdentity
// (or CertificateIdentityRegexp), via config fields.
// Without them, verification cannot establish whose signature is being accepted, making the
// verification meaningless from a supply-chain security perspective. This mirrors cosign's own
// requirement for --certificate-oidc-issuer and --certificate-identity on keyless verify.
//
// Trust material (trusted root) is resolved from credentials, not from this
// config. See the handler package for resolution order.
//
// See https://docs.sigstore.dev/cosign/verifying/verify/ for cosign verification documentation.
//
// +ocm:typegen=true
// +ocm:jsonschema-gen=true
// +k8s:deepcopy-gen:interfaces=ocm.software/open-component-model/bindings/go/runtime.Typed
// +k8s:deepcopy-gen=true
type VerifyConfig struct {
	// Type identifies this configuration object's runtime type.
	// +ocm:jsonschema-gen:enum=SigstoreVerificationConfiguration/v1alpha1
	Type runtime.Type `json:"type"`

	// PrivateInfrastructure skips public transparency log verification.
	// Use this when verifying artifacts signed by a privately deployed Sigstore
	// infrastructure where the transparency log is not the public-good Rekor instance.
	// Maps to cosign --private-infrastructure.
	PrivateInfrastructure bool `json:"privateInfrastructure,omitempty"`

	// CertificateOIDCIssuer is the exact OIDC issuer URL that the signing certificate must have
	// been issued for. Required for keyless verification unless CertificateOIDCIssuerRegexp is set.
	// Example: "https://accounts.google.com" or "https://github.com/login/oauth"
	// Maps to cosign --certificate-oidc-issuer.
	CertificateOIDCIssuer string `json:"certificateOIDCIssuer,omitempty"`

	// CertificateOIDCIssuerRegexp is a regular expression matched against the OIDC issuer URL.
	// Required for keyless verification unless CertificateOIDCIssuer is set.
	// Maps to cosign --certificate-oidc-issuer-regexp.
	CertificateOIDCIssuerRegexp string `json:"certificateOIDCIssuerRegexp,omitempty"`

	// CertificateIdentity is the exact Subject Alternative Name that the signing certificate
	// must carry. Typically the signer's email or CI workflow URI.
	// Required for keyless verification unless CertificateIdentityRegexp is set.
	// Maps to cosign --certificate-identity.
	CertificateIdentity string `json:"certificateIdentity,omitempty"`

	// CertificateIdentityRegexp is a regular expression matched against the certificate Subject
	// Alternative Name. Required for keyless verification unless CertificateIdentity is set.
	// Maps to cosign --certificate-identity-regexp.
	CertificateIdentityRegexp string `json:"certificateIdentityRegexp,omitempty"`
}

// Validate checks that SignConfig fields are well-formed.
func (c *SignConfig) Validate() error {
	return nil
}

// Validate checks that VerifyConfig fields are well-formed.
func (c *VerifyConfig) Validate() error {
	hasIssuer := c.CertificateOIDCIssuer != "" || c.CertificateOIDCIssuerRegexp != ""
	hasIdentity := c.CertificateIdentity != "" || c.CertificateIdentityRegexp != ""
	if !hasIssuer || !hasIdentity {
		return fmt.Errorf("keyless verification requires both an issuer constraint " +
			"(CertificateOIDCIssuer or CertificateOIDCIssuerRegexp) and an identity constraint " +
			"(CertificateIdentity or CertificateIdentityRegexp)")
	}
	if c.CertificateOIDCIssuer != "" {
		if err := validateURL("CertificateOIDCIssuer", c.CertificateOIDCIssuer); err != nil {
			return err
		}
	}
	if c.CertificateOIDCIssuerRegexp != "" {
		if _, err := regexp.Compile(c.CertificateOIDCIssuerRegexp); err != nil {
			return fmt.Errorf("CertificateOIDCIssuerRegexp: invalid regexp %q: %w", c.CertificateOIDCIssuerRegexp, err)
		}
	}
	if c.CertificateIdentityRegexp != "" {
		if _, err := regexp.Compile(c.CertificateIdentityRegexp); err != nil {
			return fmt.Errorf("CertificateIdentityRegexp: invalid regexp %q: %w", c.CertificateIdentityRegexp, err)
		}
	}
	return nil
}

func validateURL(field, rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%s: invalid URL %q: %w", field, rawURL, err)
	}
	if u.Host == "" {
		return fmt.Errorf("%s: URL %q has no host", field, rawURL)
	}
	return nil
}
