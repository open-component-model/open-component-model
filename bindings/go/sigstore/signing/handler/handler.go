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
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	sigcredentials "ocm.software/open-component-model/bindings/go/sigstore/signing/handler/internal/credentials"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

var _ signing.Handler = (*Handler)(nil)

const (
	IdentityAttributeAlgorithm = "algorithm"
	IdentityAttributeSignature = "signature"

	CredentialKeyOIDCToken           = "token"
	CredentialKeyTrustedRootJSON     = "trusted_root_json"
	CredentialKeyTrustedRootJSONFile = CredentialKeyTrustedRootJSON + "_file"
)

// Handler implements signing.Handler by delegating to the cosign CLI.
type Handler struct {
	executor Executor
}

// New returns a Handler that uses the default cosign executor.
// Binary resolution is lazy — on first Sign/Verify call, it looks for cosign
// on PATH and falls back to auto-downloading the pinned version if not found.
// Call Ensure() after New() for fail-fast behavior at startup.
func New() *Handler {
	return &Handler{executor: NewDefaultExecutor()}
}

// Ensure resolves the cosign binary eagerly. Call after New() when you want
// startup-time errors rather than first-use errors.
func (h *Handler) Ensure(ctx context.Context) error {
	return h.executor.Ensure(ctx)
}

// NewWithExecutor returns a Handler with a custom executor (for testing).
func NewWithExecutor(exec Executor) *Handler {
	if exec == nil {
		panic("NewWithExecutor: executor must not be nil")
	}
	return &Handler{executor: exec}
}

// GetSigningHandlerScheme returns the runtime.Scheme containing registered config types.
func (h *Handler) GetSigningHandlerScheme() *runtime.Scheme {
	return v1alpha1.Scheme
}

func (h *Handler) Sign(
	ctx context.Context,
	unsigned descruntime.Digest,
	rawCfg runtime.Typed,
	creds map[string]string,
) (descruntime.SignatureInfo, error) {
	var cfg v1alpha1.SignConfig
	if err := v1alpha1.Scheme.Convert(rawCfg, &cfg); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("convert config: %w", err)
	}

	token := strings.TrimSpace(creds[CredentialKeyOIDCToken])
	if token == "" {
		return descruntime.SignatureInfo{}, fmt.Errorf("OIDC identity token required for signing: " +
			"configure a consumer identity of type SigstoreSigner/v1alpha1 with either " +
			"a direct credential (Credentials/v1) providing the \"token\" key, or a " +
			"credential plugin (OIDCIdentityTokenProvider/v1alpha1) that resolves one")
	}

	digestBytes, err := hex.DecodeString(unsigned.Value)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("decode digest hex value: %w", err)
	}
	if len(digestBytes) == 0 {
		return descruntime.SignatureInfo{}, fmt.Errorf("digest value must not be empty")
	}

	if err := cfg.Validate(); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("invalid signing config: %w", err)
	}

	if cfg.AllowInsecureEndpoints {
		slog.Warn("insecure endpoints enabled: HTTP URLs accepted without TLS verification")
	}

	tmpDir, err := os.MkdirTemp("", "cosign-sign-*")
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("create temp dir for sign: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			slog.Warn("failed to remove temp dir containing signing material", "path", tmpDir, "error", err)
		}
	}()

	dataPath, err := writeTemp(tmpDir, "data-*", digestBytes)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("write sign data to temp file: %w", err)
	}

	bundlePath := filepath.Join(tmpDir, "bundle.json")

	args := []string{
		"sign-blob", dataPath,
		"--bundle", bundlePath,
		"--yes",
	}
	if cfg.SigningConfig != "" {
		args = append(args, "--signing-config", cfg.SigningConfig)
	} else if cfg.FulcioURL != "" || cfg.RekorURL != "" || cfg.TimestampServerURL != "" {
		args = append(args, "--use-signing-config=false")
	}
	if cfg.FulcioURL != "" {
		args = append(args, "--fulcio-url", cfg.FulcioURL)
	}
	if cfg.RekorURL != "" {
		args = append(args, "--rekor-url", cfg.RekorURL)
	}
	if cfg.TimestampServerURL != "" {
		args = append(args, "--timestamp-server-url", cfg.TimestampServerURL)
	}
	if cfg.TrustedRoot != "" {
		args = append(args, "--trusted-root", cfg.TrustedRoot)
	}

	// Token comes from OCM credential system, not ambient env. The allowlist
	// deliberately excludes sigstore-specific env vars (SIGSTORE_*, TUF_*, COSIGN_*).
	env := append(cosignEnv(), "SIGSTORE_ID_TOKEN="+token)

	if err := h.executor.Run(ctx, args, env); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("cosign sign: %w", err)
	}

	bundleJSON, err := os.ReadFile(bundlePath)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("read bundle output: %w", err)
	}

	certInfo, err := extractCertInfoFromBundleJSON(bundleJSON)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("extract cert info from bundle: %w", err)
	}
	if certInfo.Identity == "" {
		slog.Warn("signing certificate contains no SAN identity (email or URI)")
	}
	slog.Debug("signing certificate identity", "issuer", certInfo.Issuer, "identity", certInfo.Identity)

	// MediaType is fixed: this handler produces/verifies Sigstore bundles v0.3+json (cosign >=3.0).
	return descruntime.SignatureInfo{
		Algorithm: v1alpha1.AlgorithmSigstore,
		MediaType: v1alpha1.MediaTypeSigstoreBundle,
		Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		Issuer:    certInfo.Issuer,
	}, nil
}

func (h *Handler) Verify(
	ctx context.Context,
	signed descruntime.Signature,
	rawCfg runtime.Typed,
	creds map[string]string,
) error {
	var cfg v1alpha1.VerifyConfig
	if err := v1alpha1.Scheme.Convert(rawCfg, &cfg); err != nil {
		return fmt.Errorf("convert config: %w", err)
	}

	if signed.Signature.MediaType != v1alpha1.MediaTypeSigstoreBundle {
		return fmt.Errorf("unsupported media type %q for sigstore verification", signed.Signature.MediaType)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid verification config: %w", err)
	}

	if cfg.PrivateInfrastructure && cfg.TrustedRoot == "" &&
		strings.TrimSpace(creds[CredentialKeyTrustedRootJSON]) == "" &&
		strings.TrimSpace(creds[CredentialKeyTrustedRootJSONFile]) == "" {
		return fmt.Errorf("privateInfrastructure requires a trusted root: " +
			"set TrustedRoot in the verifier config or provide a TrustedRoot credential")
	}

	if cfg.AllowInsecureEndpoints {
		slog.Warn("insecure endpoints enabled: HTTP URLs accepted without TLS verification")
	}

	if isPermissivePattern(cfg.CertificateOIDCIssuerRegexp) && isPermissivePattern(cfg.CertificateIdentityRegexp) {
		slog.Warn("verification uses trivially permissive identity patterns — "+
			"any valid Sigstore signature will pass regardless of signer identity",
			"issuerRegexp", cfg.CertificateOIDCIssuerRegexp,
			"identityRegexp", cfg.CertificateIdentityRegexp)
	}

	bundleJSON, err := base64.StdEncoding.DecodeString(signed.Signature.Value)
	if err != nil {
		return fmt.Errorf("decode bundle base64: %w", err)
	}

	digestBytes, err := hex.DecodeString(signed.Digest.Value)
	if err != nil {
		return fmt.Errorf("decode digest hex: %w", err)
	}
	if len(digestBytes) == 0 {
		return fmt.Errorf("digest value must not be empty")
	}

	tmpDir, err := os.MkdirTemp("", "cosign-verify-*")
	if err != nil {
		return fmt.Errorf("create temp dir for verify: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			slog.Warn("failed to remove temp dir containing verification material", "path", tmpDir, "error", err)
		}
	}()

	trustedRootPath, err := resolveTrustedRootPath(&cfg, creds, tmpDir)
	if err != nil {
		return fmt.Errorf("resolve trusted root: %w", err)
	}

	dataPath, err := writeTemp(tmpDir, "data-*", digestBytes)
	if err != nil {
		return fmt.Errorf("write verify data to temp file: %w", err)
	}

	bundlePath, err := writeTemp(tmpDir, "bundle-*.json", bundleJSON)
	if err != nil {
		return fmt.Errorf("write bundle to temp file: %w", err)
	}

	args := []string{"verify-blob", dataPath, "--bundle", bundlePath}
	if cfg.CertificateIdentity != "" {
		args = append(args, "--certificate-identity", cfg.CertificateIdentity)
	}
	if cfg.CertificateIdentityRegexp != "" {
		args = append(args, "--certificate-identity-regexp", cfg.CertificateIdentityRegexp)
	}
	if cfg.CertificateOIDCIssuer != "" {
		args = append(args, "--certificate-oidc-issuer", cfg.CertificateOIDCIssuer)
	}
	if cfg.CertificateOIDCIssuerRegexp != "" {
		args = append(args, "--certificate-oidc-issuer-regexp", cfg.CertificateOIDCIssuerRegexp)
	}
	if trustedRootPath != "" {
		args = append(args, "--trusted-root", trustedRootPath)
	}
	if cfg.PrivateInfrastructure {
		args = append(args, "--private-infrastructure")
	}

	if err := h.executor.Run(ctx, args, cosignEnv()); err != nil {
		return fmt.Errorf("verify signature: %w", err)
	}

	return nil
}

func (*Handler) GetSigningCredentialConsumerIdentity(
	_ context.Context,
	name string,
	_ descruntime.Digest,
	rawCfg runtime.Typed,
) (runtime.Identity, error) {
	var cfg v1alpha1.SignConfig
	if err := v1alpha1.Scheme.Convert(rawCfg, &cfg); err != nil {
		return nil, fmt.Errorf("convert config: %w", err)
	}
	id := credentialIdentity(sigcredentials.IdentityTypeSigstoreSigner)
	id[IdentityAttributeSignature] = name
	return id, nil
}

func (*Handler) GetVerifyingCredentialConsumerIdentity(
	_ context.Context,
	signature descruntime.Signature,
	_ runtime.Typed,
) (runtime.Identity, error) {
	if signature.Signature.MediaType != v1alpha1.MediaTypeSigstoreBundle {
		return nil, fmt.Errorf("unsupported media type %q for sigstore verification", signature.Signature.MediaType)
	}
	id := credentialIdentity(sigcredentials.IdentityTypeSigstoreVerifier)
	id[IdentityAttributeSignature] = signature.Name
	return id, nil
}

func credentialIdentity(identityType runtime.Type) runtime.Identity {
	id := runtime.Identity{IdentityAttributeAlgorithm: v1alpha1.AlgorithmSigstore}
	id.SetType(identityType)
	return id
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
	if jsonVal := strings.TrimSpace(creds[CredentialKeyTrustedRootJSON]); jsonVal != "" {
		path, err := writeTemp(tmpDir, "cosign-trusted-root-*.json", []byte(jsonVal))
		if err != nil {
			return "", fmt.Errorf("write trusted root to temp file: %w", err)
		}
		return path, nil
	}

	if filePath := strings.TrimSpace(creds[CredentialKeyTrustedRootJSONFile]); filePath != "" {
		if err := validateTrustedRootPath(filePath); err != nil {
			return "", err
		}
		return filePath, nil
	}

	if cfg.TrustedRoot != "" {
		return cfg.TrustedRoot, nil
	}

	return "", nil
}

func validateTrustedRootPath(p string) error {
	if !filepath.IsAbs(p) {
		return fmt.Errorf("trusted root file path must be absolute, got %q", p)
	}
	if cleaned := filepath.Clean(p); cleaned != p {
		return fmt.Errorf("trusted root file path contains non-canonical elements (e.g. ..): %q", p)
	}
	return nil
}

var permissivePatterns = map[string]bool{
	".*": true, ".+": true, "^.*$": true, "^.+$": true,
}

func isPermissivePattern(pattern string) bool {
	return permissivePatterns[pattern]
}

var (
	sigstoreIssuerV1OID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}
	sigstoreIssuerV2OID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}
)

type bundleCertInfo struct {
	Issuer   string
	Identity string // SAN: first email or URI from Fulcio cert
}

func extractCertInfoFromBundleJSON(bundleJSON []byte) (bundleCertInfo, error) {
	var bundle struct {
		VerificationMaterial struct {
			Certificate struct {
				RawBytes string `json:"rawBytes"`
			} `json:"certificate"`
		} `json:"verificationMaterial"`
	}
	if err := json.Unmarshal(bundleJSON, &bundle); err != nil {
		return bundleCertInfo{}, fmt.Errorf("unmarshal bundle JSON: %w", err)
	}

	certDER, err := base64.StdEncoding.DecodeString(bundle.VerificationMaterial.Certificate.RawBytes)
	if err != nil {
		return bundleCertInfo{}, fmt.Errorf("decode certificate base64: %w", err)
	}
	if len(certDER) == 0 {
		return bundleCertInfo{}, errors.New("bundle contains no certificate")
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return bundleCertInfo{}, fmt.Errorf("parse Fulcio certificate: %w", err)
	}

	var identity string
	if len(cert.EmailAddresses) > 0 {
		identity = cert.EmailAddresses[0]
	} else if len(cert.URIs) > 0 {
		identity = cert.URIs[0].String()
	}

	var v1Issuer string
	var v2Err error
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(sigstoreIssuerV2OID) {
			var issuer string
			if _, err := asn1.Unmarshal(ext.Value, &issuer); err == nil {
				return bundleCertInfo{Issuer: issuer, Identity: identity}, nil
			} else {
				v2Err = err
			}
		}
		if v1Issuer == "" && ext.Id.Equal(sigstoreIssuerV1OID) {
			v1Issuer = string(ext.Value)
		}
	}

	if v1Issuer != "" {
		return bundleCertInfo{Issuer: v1Issuer, Identity: identity}, nil
	}

	if v2Err != nil {
		return bundleCertInfo{}, fmt.Errorf("fulcio certificate: V2 issuer extension (OID %s) present but malformed: %w", sigstoreIssuerV2OID, v2Err)
	}
	return bundleCertInfo{}, fmt.Errorf("fulcio certificate contains no issuer extension (OID %s or %s)", sigstoreIssuerV1OID, sigstoreIssuerV2OID)
}
