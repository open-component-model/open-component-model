package handler

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/signing"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/handler/internal"
	sigcredentials "ocm.software/open-component-model/bindings/go/sigstore/signing/handler/internal/credentials"
	"ocm.software/open-component-model/bindings/go/sigstore/signing/v1alpha1"
)

var _ signing.Handler = (*Handler)(nil)

const (
	IdentityAttributeSignature = "signature"
	IdentityAttributeIssuer    = "issuer"
	IdentityAttributeClientID  = "clientID"

	CredentialKeyOIDCToken           = "token"
	CredentialKeyTrustedRootJSON     = "trusted_root_json"
	CredentialKeyTrustedRootJSONFile = CredentialKeyTrustedRootJSON + "_file"
)

// Handler implements signing.Handler by delegating to the cosign CLI.
// Safe for concurrent use. Binary resolution happens lazily on first Sign or Verify call.
type Handler struct {
	runner  *internal.CosignBinary
	tempDir string
}

// New creates a Handler. Binary resolution happens lazily on first Sign or Verify call.
func New(opts ...HandlerOption) *Handler {
	h := &Handler{runner: internal.NewCosignBinary()}
	for _, opt := range opts {
		opt(h)
	}
	if h.tempDir == "" {
		h.tempDir = os.TempDir()
	}
	return h
}

// GetSigningHandlerScheme returns the runtime.Scheme containing registered config types.
func (h *Handler) GetSigningHandlerScheme() *runtime.Scheme {
	return v1alpha1.Scheme
}

// Sign performs keyless signing via cosign sign-blob: resolves an OIDC token,
// invokes Fulcio for a short-lived certificate, signs the digest, records in Rekor,
// and returns the base64-encoded Sigstore bundle with extracted issuer/identity.
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

	if err := cfg.Validate(); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("invalid signing config: %w", err)
	}

	if strings.HasPrefix(cfg.Issuer, "http://") {
		slog.WarnContext(ctx, "Issuer uses HTTP (non-TLS); this is insecure outside of test environments")
	}

	digestBytes, err := hex.DecodeString(unsigned.Value)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("decode digest hex value: %w", err)
	}
	if len(digestBytes) == 0 {
		return descruntime.SignatureInfo{}, fmt.Errorf("digest value must not be empty")
	}

	env := os.Environ()
	if !internal.HasEnvKey(env, "SIGSTORE_ID_TOKEN") && !internal.HasEnvKey(env, "ACTIONS_ID_TOKEN_REQUEST_TOKEN") {
		token := strings.TrimSpace(creds[CredentialKeyOIDCToken])
		if token == "" {
			return descruntime.SignatureInfo{}, fmt.Errorf("OIDC identity token required: " +
				"set SIGSTORE_ID_TOKEN env var, use GitHub Actions OIDC, " +
				"or configure an OIDCIdentityToken credential")
		}
		env = append(env, "SIGSTORE_ID_TOKEN="+token)
	}

	tmpDir, err := os.MkdirTemp(h.tempDir, "cosign-sign-*")
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("create temp dir for sign: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			slog.WarnContext(ctx, "failed to remove temp dir containing signing material", "path", tmpDir, "error", err)
		}
	}()

	dataPath, err := writeTemp(tmpDir, "data-*", bytes.NewReader(digestBytes))
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("write sign data to temp file: %w", err)
	}

	bundlePath := filepath.Join(tmpDir, "bundle.json")

	trustedRootPath, err := resolveTrustedRootPath(creds, tmpDir)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("resolve trusted root: %w", err)
	}

	var extraArgs []string
	if cfg.SigningConfig != "" {
		extraArgs = append(extraArgs, "--signing-config", cfg.SigningConfig)
	}
	if trustedRootPath != "" {
		extraArgs = append(extraArgs, "--trusted-root", trustedRootPath)
	}

	if err := h.runner.Sign(ctx, dataPath, bundlePath, extraArgs, env); err != nil {
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
		slog.WarnContext(ctx, "signing certificate contains no SAN identity (email or URI)")
	}
	slog.DebugContext(ctx, "signing certificate identity", "issuer", certInfo.Issuer, "identity", certInfo.Identity)

	// MediaType is fixed: this handler produces/verifies Sigstore bundles v0.3+json (cosign >=3.0).
	return descruntime.SignatureInfo{
		Algorithm: v1alpha1.AlgorithmSigstore,
		MediaType: v1alpha1.MediaTypeSigstoreBundle,
		Value:     base64.StdEncoding.EncodeToString(bundleJSON),
		Issuer:    certInfo.Issuer,
	}, nil
}

// Verify checks a Sigstore bundle via cosign verify-blob: decodes the bundle and digest,
// validates the Fulcio certificate chain and Rekor inclusion proof, and confirms the
// signed content matches the digest using the configured identity/issuer constraints.
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

	if cfg.PrivateInfrastructure &&
		strings.TrimSpace(creds[CredentialKeyTrustedRootJSON]) == "" &&
		strings.TrimSpace(creds[CredentialKeyTrustedRootJSONFile]) == "" {
		return fmt.Errorf("privateInfrastructure requires a trusted root: " +
			"provide a TrustedRoot credential (trusted_root_json or trusted_root_json_file)")
	}

	if strings.HasPrefix(cfg.CertificateOIDCIssuer, "http://") {
		slog.WarnContext(ctx, "CertificateOIDCIssuer uses HTTP (non-TLS); this is insecure outside of test environments")
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

	tmpDir, err := os.MkdirTemp(h.tempDir, "cosign-verify-*")
	if err != nil {
		return fmt.Errorf("create temp dir for verify: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			slog.WarnContext(ctx, "failed to remove temp dir containing verification material", "path", tmpDir, "error", err)
		}
	}()

	trustedRootPath, err := resolveTrustedRootPath(creds, tmpDir)
	if err != nil {
		return fmt.Errorf("resolve trusted root: %w", err)
	}

	dataPath, err := writeTemp(tmpDir, "data-*", bytes.NewReader(digestBytes))
	if err != nil {
		return fmt.Errorf("write verify data to temp file: %w", err)
	}

	bundlePath, err := writeTemp(tmpDir, "bundle-*.json", bytes.NewReader(bundleJSON))
	if err != nil {
		return fmt.Errorf("write bundle to temp file: %w", err)
	}

	var extraArgs []string
	if cfg.CertificateIdentity != "" {
		extraArgs = append(extraArgs, "--certificate-identity", cfg.CertificateIdentity)
	}
	if cfg.CertificateIdentityRegexp != "" {
		extraArgs = append(extraArgs, "--certificate-identity-regexp", cfg.CertificateIdentityRegexp)
	}
	if cfg.CertificateOIDCIssuer != "" {
		extraArgs = append(extraArgs, "--certificate-oidc-issuer", cfg.CertificateOIDCIssuer)
	}
	if cfg.CertificateOIDCIssuerRegexp != "" {
		extraArgs = append(extraArgs, "--certificate-oidc-issuer-regexp", cfg.CertificateOIDCIssuerRegexp)
	}
	if trustedRootPath != "" {
		extraArgs = append(extraArgs, "--trusted-root", trustedRootPath)
	}
	if cfg.PrivateInfrastructure {
		extraArgs = append(extraArgs, "--private-infrastructure")
	}

	if err := h.runner.Verify(ctx, dataPath, bundlePath, extraArgs, os.Environ()); err != nil {
		return err
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
	if cfg.Issuer != "" {
		id[IdentityAttributeIssuer] = cfg.Issuer
	}
	if cfg.ClientID != "" {
		id[IdentityAttributeClientID] = cfg.ClientID
	}
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
	id := runtime.Identity{}
	id.SetType(identityType)
	return id
}

// resolveTrustedRootPath returns a path to the trusted root JSON, or ""
// if no trusted root is configured (cosign falls back to public-good TUF).
//
// Resolution order (first non-empty wins):
//  1. Inline JSON from credentials (written to a temp file, cleaned up by caller's defer os.RemoveAll(tmpDir))
//  2. File path from credentials (not removed on cleanup)
//  3. "" — cosign falls back to public-good TUF
func resolveTrustedRootPath(creds map[string]string, tmpDir string) (string, error) {
	if jsonVal := strings.TrimSpace(creds[CredentialKeyTrustedRootJSON]); jsonVal != "" {
		path, err := writeTemp(tmpDir, "cosign-trusted-root-*.json", strings.NewReader(jsonVal))
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

// Fulcio embeds the OIDC issuer URL in Sigstore bundle certificates as proprietary X.509
// extensions under the Sigstore PEN (Private Enterprise Number 57264).
// See https://github.com/sigstore/fulcio/blob/main/docs/oid-info.md for the full OID registry.
//
// V1 (OID .1): issuer stored as raw UTF-8 bytes directly in the extension value.
// V2 (OID .8): issuer stored as an ASN.1 DER-encoded UTF8String (requires asn1.Unmarshal).
// We prefer V2 and fall back to V1 for compatibility with older Fulcio deployments.
var (
	sigstoreIssuerV1OID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 1}
	sigstoreIssuerV2OID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 57264, 1, 8}
)

type bundleCertInfo struct {
	Issuer   string
	Identity string // SAN: first email or URI from Fulcio cert
}

type sigstoreBundle struct {
	VerificationMaterial struct {
		Certificate struct {
			RawBytes string `json:"rawBytes"`
		} `json:"certificate"`
	} `json:"verificationMaterial"`
}

// extractCertInfoFromBundleJSON extracts the OIDC issuer and signer identity from
// the Fulcio certificate embedded in a Sigstore bundle.
// The caller needs these to populate SignatureInfo so consumers can attribute a signature
// to a specific identity without re-parsing the bundle.
// Tries the V2 issuer OID first, falls back to V1 for older Fulcio deployments.
func extractCertInfoFromBundleJSON(bundleJSON []byte) (bundleCertInfo, error) {
	var bundle sigstoreBundle
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

func writeTemp(dir, pattern string, r io.Reader) (path string, err error) {
	f, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create temp file %q: %w", pattern, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close temp file %q: %w", pattern, cerr)
		}
		if err != nil {
			_ = os.Remove(f.Name())
		}
	}()
	if _, err = io.Copy(f, r); err != nil {
		return "", fmt.Errorf("write temp file %q: %w", pattern, err)
	}
	return f.Name(), nil
}
