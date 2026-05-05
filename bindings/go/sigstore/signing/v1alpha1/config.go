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
//     discovery. Explicit URL fields below supplement/override endpoints from the file.
//  2. Explicit URL fields only (no SigningConfig) — passed directly to cosign
//     with --use-signing-config=false to disable TUF-based auto-discovery.
//  3. Neither set — cosign's default (--use-signing-config=true) fetches
//     the signing config from the public-good Sigstore TUF repository.
//
// When an OIDC token is available in credentials, it is forwarded to cosign
// via the SIGSTORE_ID_TOKEN environment variable. A token is required;
// the handler returns an error if no token credential is resolved.
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
	// this file instead of using the individual URL fields or TUF auto-discovery.
	// Maps to cosign --signing-config.
	SigningConfig string `json:"signingConfig,omitempty"`

	// FulcioURL is the URL of the Fulcio certificate authority for keyless signing.
	// Maps to cosign --fulcio-url.
	FulcioURL string `json:"fulcioURL,omitempty"`

	// RekorURL is the URL of the Rekor transparency log.
	// Maps to cosign --rekor-url.
	RekorURL string `json:"rekorURL,omitempty"`

	// TimestampServerURL is the full URL of a RFC 3161 Timestamp Authority endpoint.
	// Maps to cosign --timestamp-server-url.
	TimestampServerURL string `json:"timestampServerURL,omitempty"`

	// TrustedRoot is a filesystem path to a trusted root JSON file.
	// When set during signing, cosign validates the Fulcio certificate chain
	// against this root instead of the public-good Sigstore TUF root.
	// Required when signing against a privately deployed Sigstore infrastructure
	// (e.g. a private Fulcio CA without a CT log).
	// Maps to cosign --trusted-root.
	TrustedRoot string `json:"trustedRoot,omitempty"`

	// AllowInsecureEndpoints permits HTTP (non-TLS) endpoint URLs for Fulcio,
	// Rekor, and TSA. Default false enforces HTTPS for all service communication.
	// Only use when the network path is otherwise secured (e.g. loopback, mTLS
	// overlay, air-gapped cluster, or local development/testing).
	AllowInsecureEndpoints bool `json:"allowInsecureEndpoints,omitempty"`
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

	// TrustedRoot is a filesystem path to a trusted root JSON file for offline verification.
	// When omitted, cosign uses the public-good Sigstore TUF root.
	// Maps to cosign --trusted-root.
	TrustedRoot string `json:"trustedRoot,omitempty"`

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

	// AllowInsecureEndpoints permits HTTP (non-TLS) URLs in CertificateOIDCIssuer.
	// Default false enforces HTTPS. Only enable for testing or air-gapped environments.
	AllowInsecureEndpoints bool `json:"allowInsecureEndpoints,omitempty"`
}

// Validate checks that SignConfig fields are well-formed.
func (c *SignConfig) Validate() error {
	if c.TrustedRoot != "" && c.SigningConfig == "" &&
		c.FulcioURL == "" && c.RekorURL == "" && c.TimestampServerURL == "" {
		return fmt.Errorf("trustedRoot specifies whom to trust but no signing " +
			"infrastructure is configured; set signingConfig or explicit endpoint URLs " +
			"(fulcioURL, rekorURL, timestampServerURL)")
	}
	for _, u := range []struct{ field, val string }{
		{"FulcioURL", c.FulcioURL},
		{"RekorURL", c.RekorURL},
		{"TimestampServerURL", c.TimestampServerURL},
	} {
		if c.AllowInsecureEndpoints {
			if err := validateURL(u.field, u.val); err != nil {
				return err
			}
		} else {
			if err := validateHTTPS(u.field, u.val); err != nil {
				return err
			}
		}
	}
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
		if c.AllowInsecureEndpoints {
			if err := validateURL("CertificateOIDCIssuer", c.CertificateOIDCIssuer); err != nil {
				return err
			}
		} else {
			if err := validateHTTPS("CertificateOIDCIssuer", c.CertificateOIDCIssuer); err != nil {
				return err
			}
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

func validateHTTPS(field, rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%s: invalid URL %q: %w", field, rawURL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%s: URL %q must use https scheme", field, rawURL)
	}
	if u.Host == "" {
		return fmt.Errorf("%s: URL %q has no host", field, rawURL)
	}
	return nil
}
