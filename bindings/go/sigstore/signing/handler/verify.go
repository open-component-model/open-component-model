package handler

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

func doVerify(
	ctx context.Context,
	signed descruntime.Signature,
	cfg *v1alpha1.VerifyConfig,
	creds map[string]string,
	executor Executor,
) error {
	if signed.Signature.MediaType != v1alpha1.MediaTypeSigstoreBundle {
		return fmt.Errorf("unsupported media type %q for sigstore verification", signed.Signature.MediaType)
	}

	if !hasIdentityConfig(cfg) {
		return fmt.Errorf("keyless verification requires both an issuer constraint and an identity constraint: " +
			"set CertificateOIDCIssuer (or CertificateOIDCIssuerRegexp) AND CertificateIdentity (or CertificateIdentityRegexp) " +
			"in the verifier spec")
	}

	if cfg.PrivateInfrastructure && cfg.TrustedRoot == "" && creds[CredentialKeyTrustedRootJSON] == "" && creds[CredentialKeyTrustedRootJSONFile] == "" {
		return fmt.Errorf("privateInfrastructure requires a trusted root: " +
			"set TrustedRoot in the verifier config or provide a TrustedRoot credential")
	}

	if cfg.CertificateOIDCIssuer != "" {
		if err := validateHTTPSURL("CertificateOIDCIssuer", cfg.CertificateOIDCIssuer); err != nil {
			return err
		}
	}

	tmpDir, err := os.MkdirTemp("", "cosign-verify-*")
	if err != nil {
		return fmt.Errorf("create temp dir for verify: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	bundleJSON, err := base64.StdEncoding.DecodeString(signed.Signature.Value)
	if err != nil {
		return fmt.Errorf("decode bundle base64: %w", err)
	}

	bundlePath, err := writeTemp(tmpDir, "cosign-verify-bundle-*.json", bundleJSON)
	if err != nil {
		return fmt.Errorf("write bundle to temp file: %w", err)
	}

	digestBytes, err := hex.DecodeString(signed.Digest.Value)
	if err != nil {
		return fmt.Errorf("decode digest hex: %w", err)
	}

	trustedRootPath, err := resolveTrustedRootPath(cfg, creds, tmpDir)
	if err != nil {
		return fmt.Errorf("resolve trusted root: %w", err)
	}

	opts := VerifyOpts{
		CertificateIdentity:         cfg.CertificateIdentity,
		CertificateIdentityRegexp:   cfg.CertificateIdentityRegexp,
		CertificateOIDCIssuer:       cfg.CertificateOIDCIssuer,
		CertificateOIDCIssuerRegexp: cfg.CertificateOIDCIssuerRegexp,
		TrustedRoot:                 trustedRootPath,
		PrivateInfrastructure:       cfg.PrivateInfrastructure,
	}

	if err := executor.VerifyData(ctx, digestBytes, bundlePath, opts); err != nil {
		return fmt.Errorf("verify signature: %w", err)
	}

	return nil
}

// resolveTrustedRootPath returns a path to the trusted root JSON, or ""
// if no trusted root is configured (cosign falls back to public-good TUF).
//
// Resolution order (first non-empty wins):
//  1. Inline JSON from credentials (written to a temp file, cleaned up by caller's defer os.RemoveAll(tmpDir))
//  2. File path from credentials (not removed on cleanup)
//  3. Config field value (not removed on cleanup)
//  4. "" — cosign falls back to public-good TUF
func resolveTrustedRootPath(cfg *v1alpha1.VerifyConfig, creds map[string]string, tmpDir string) (string, error) {
	if jsonVal := creds[CredentialKeyTrustedRootJSON]; jsonVal != "" {
		path, err := writeTemp(tmpDir, "cosign-trusted-root-*.json", []byte(jsonVal))
		if err != nil {
			return "", fmt.Errorf("write trusted root to temp file: %w", err)
		}
		return path, nil
	}

	if filePath := creds[CredentialKeyTrustedRootJSONFile]; filePath != "" {
		return filePath, nil
	}

	if cfg.TrustedRoot != "" {
		return cfg.TrustedRoot, nil
	}

	return "", nil
}

// hasIdentityConfig checks that both an issuer constraint AND an identity constraint are set.
// This is a pre-check that mirrors cosign's own requirement: without --certificate-oidc-issuer
// and --certificate-identity (or their regex variants), cosign verify-blob will refuse to run.
// Failing early here gives the user a clearer error than the raw cosign output.
func hasIdentityConfig(cfg *v1alpha1.VerifyConfig) bool {
	hasIssuer := cfg.CertificateOIDCIssuer != "" || cfg.CertificateOIDCIssuerRegexp != ""
	hasIdentity := cfg.CertificateIdentity != "" || cfg.CertificateIdentityRegexp != ""
	return hasIssuer && hasIdentity
}
