package handler

import (
	"context"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

func doSign(
	ctx context.Context,
	unsigned descruntime.Digest,
	cfg *v1alpha1.SignConfig,
	creds map[string]string,
	executor Executor,
) (descruntime.SignatureInfo, error) {
	token := creds[CredentialKeyOIDCToken]
	if token == "" {
		return descruntime.SignatureInfo{}, fmt.Errorf("OIDC identity token required for signing: " +
			"configure a consumer identity of type OIDCIdentityToken/v1alpha1 with a " +
			"OIDCProvider/v1alpha1 credential in .ocmconfig")
	}

	digestBytes, err := hex.DecodeString(unsigned.Value)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("decode digest hex value: %w", err)
	}

	for _, u := range []struct{ field, val string }{
		{"FulcioURL", cfg.FulcioURL},
		{"RekorURL", cfg.RekorURL},
		{"TimestampServerURL", cfg.TimestampServerURL},
	} {
		if err := validateHTTPSURL(u.field, u.val); err != nil {
			return descruntime.SignatureInfo{}, err
		}
	}

	opts := SignOpts{
		IdentityToken:      token,
		SigningConfig:      cfg.SigningConfig,
		FulcioURL:          cfg.FulcioURL,
		RekorURL:           cfg.RekorURL,
		TimestampServerURL: cfg.TimestampServerURL,
		TrustedRoot:        cfg.TrustedRoot,
	}

	bundleJSON, err := executor.SignData(ctx, digestBytes, opts)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("cosign sign: %w", err)
	}

	issuer, err := extractIssuerFromBundleJSON(bundleJSON)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("extract issuer from bundle: %w", err)
	}

	return descruntime.SignatureInfo{
		Algorithm: v1alpha1.AlgorithmSigstore,
		MediaType: v1alpha1.MediaTypeSigstoreBundle,
		Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		Issuer:    issuer,
	}, nil
}

// sigstoreIssuerV1OID is the Fulcio OIDC issuer extension (v1, deprecated).
// OID 1.3.6.1.4.1.57264.1.1 — raw UTF-8 string.
var sigstoreIssuerV1OID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}

// sigstoreIssuerV2OID is the Fulcio OIDC issuer extension (v2).
// OID 1.3.6.1.4.1.57264.1.8 — ASN.1 UTF8String.
var sigstoreIssuerV2OID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}

// extractIssuerFromBundleJSON parses a Sigstore bundle JSON to extract the OIDC
// issuer from the Fulcio certificate, using only standard library types.
// It returns an error if the issuer cannot be determined, because a signature
// without a verifiable issuer is not useful for identity-based verification.
func extractIssuerFromBundleJSON(bundleJSON []byte) (string, error) {
	var bundle struct {
		VerificationMaterial struct {
			Certificate struct {
				RawBytes string `json:"rawBytes"`
			} `json:"certificate"`
		} `json:"verificationMaterial"`
	}
	if err := json.Unmarshal(bundleJSON, &bundle); err != nil {
		return "", fmt.Errorf("unmarshal bundle JSON: %w", err)
	}

	certDER, err := base64.StdEncoding.DecodeString(bundle.VerificationMaterial.Certificate.RawBytes)
	if err != nil {
		return "", fmt.Errorf("decode certificate base64: %w", err)
	}
	if len(certDER) == 0 {
		return "", errors.New("bundle contains no certificate")
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return "", fmt.Errorf("parse Fulcio certificate: %w", err)
	}

	// Prefer V2 OID over V1. Scan all extensions first for V2; fall back to V1.
	var v1Issuer string
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(sigstoreIssuerV2OID) {
			var issuer string
			if _, err := asn1.Unmarshal(ext.Value, &issuer); err == nil {
				return issuer, nil
			}
		}
		if v1Issuer == "" && ext.Id.Equal(sigstoreIssuerV1OID) {
			v1Issuer = string(ext.Value)
		}
	}

	if v1Issuer == "" {
		return "", errors.New("fulcio certificate contains no issuer extension (OID 1.3.6.1.4.1.57264.1.1 or 1.3.6.1.4.1.57264.1.8)")
	}
	return v1Issuer, nil
}
