// Package handler implements RSA signing and verification for OCM.
// It supports both RSASSA-PSS and RSASSA-PKCS1-v1_5, and two encodings:
//  1. Plain: hex signature bytes without certificates.
//  2. PEM: a SIGNATURE PEM block with an embedded X.509 chain.
//
// For PEM verification, the leaf public key is taken from the chain after
// the chain validates against system roots and/or an optional trust anchor
// provided via credentials.
package handler

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	rsacredentials "ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/credentials"
	rsasignature "ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/pem"
	"ocm.software/open-component-model/bindings/go/rsa/signing/handler/internal/rfc2253"
	"ocm.software/open-component-model/bindings/go/rsa/signing/v1alpha1"
	rsacredentialsv1 "ocm.software/open-component-model/bindings/go/rsa/spec/credentials/v1"
	identityv1 "ocm.software/open-component-model/bindings/go/rsa/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

const (
	// IdentityAttributeAlgorithm will be removed in the future
	//
	// Deprecated: Use typed identity [identityv1.RSAIdentity] instead
	IdentityAttributeAlgorithm = identityv1.IdentityAttributeAlgorithm

	// IdentityAttributeSignature will be removed in the future
	//
	// Deprecated: Use typed identity [identityv1.RSAIdentity] instead
	IdentityAttributeSignature = identityv1.IdentityAttributeSignature
)

// Common errors for callers to test.
var (
	ErrInvalidAlgorithm  = errors.New("invalid algorithm")
	ErrMissingPrivateKey = errors.New("private key not found")
	ErrMissingPublicKey  = errors.New("missing public key, required for plain RSA signatures")
)

// Handler holds trust anchors and time source for X.509 validation.
type Handler struct {
	roots *x509.CertPool
	now   func() time.Time
}

// New returns a Handler. If useSystemRoots is true, system trust roots are loaded, otherwise an empty pool is used.
func New(scheme *runtime.Scheme, useSystemRoots bool) (*Handler, error) {
	var (
		roots *x509.CertPool
		err   error
	)
	if useSystemRoots {
		roots, err = x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system roots: %w", err)
		}
	}
	return &Handler{
		roots: roots,
		now:   time.Now,
	}, nil
}

func (h *Handler) GetSigningHandlerScheme() *runtime.Scheme {
	return v1alpha1.Scheme
}

// ---- SPI ----

// Sign produces a signature for the given digest, using RSA and the configured
// algorithm and encoding policy. For PEM encoding, the certificate chain is
// read from credentials and embedded into the SIGNATURE block.
func (h *Handler) Sign(
	ctx context.Context,
	unsigned descruntime.Digest,
	rawCfg runtime.Typed,
	creds map[string]string,
) (descruntime.SignatureInfo, error) {
	var supported v1alpha1.Config
	if err := h.GetSigningHandlerScheme().Convert(rawCfg, &supported); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("convert config: %w", err)
	}
	algorithm := supported.GetSignatureAlgorithm()
	typedCreds := rsacredentialsv1.FromDirectCredentials(creds)

	priv, err := rsacredentials.PrivateKeyFromCredentials(typedCreds)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("cannot load private key from credentials for signing: %w", err)
	}
	if priv == nil {
		return descruntime.SignatureInfo{}, ErrMissingPrivateKey
	}

	hash, dig, err := parseDigest(unsigned)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}

	rawSig, err := signRSA(algorithm, priv, hash, dig)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("rsa sign: %w", err)
	}

	switch supported.GetSignatureEncodingPolicy() {
	case v1alpha1.SignatureEncodingPolicyPEM:
		slog.WarnContext(ctx, "signing with PEM encoding is experimental")
		chain, err := rsacredentials.CertificateChainFromCredentials(typedCreds)
		if err != nil {
			return descruntime.SignatureInfo{}, fmt.Errorf("read certificate chain: %w", err)
		}
		pem := rsasignature.SignatureBytesToPem(string(algorithm), rawSig, chain...)
		return descruntime.SignatureInfo{
			Algorithm: string(algorithm),
			MediaType: v1alpha1.MediaTypePEM,
			Value:     string(pem),
		}, nil
	case v1alpha1.SignatureEncodingPolicyPlain:
		fallthrough
	default:
		return descruntime.SignatureInfo{
			Algorithm: string(algorithm),
			MediaType: supported.GetDefaultMediaType(),
			Value:     hex.EncodeToString(rawSig),
		}, nil
	}
}

// Verify validates an OCM signature. For plain signatures, a public key must be
// present in credentials. For PEM signatures, the embedded chain must be valid
// against system roots and/or the optional trust anchor in credentials.
func (h *Handler) Verify(
	ctx context.Context,
	signed descruntime.Signature,
	// we use hints from the signature to determine the correct settings, so no additional config is needed
	_ runtime.Typed,
	creds map[string]string,
) error {
	typedCreds := rsacredentialsv1.FromDirectCredentials(creds)
	pubFromCreds, err := rsacredentials.PublicKeyFromCredentials(typedCreds)
	if err != nil {
		return fmt.Errorf("cannot load public key from credentials for verification: %w", err)
	}

	hash, dig, err := parseDigest(signed.Digest)
	if err != nil {
		return err
	}

	switch signed.Signature.MediaType {
	case v1alpha1.MediaTypePlainRSASSAPSS, v1alpha1.MediaTypePlainRSASSAPKCS1V15:
		if pubFromCreds == nil {
			return ErrMissingPublicKey
		}
		sig, err := hex.DecodeString(signed.Signature.Value)
		if err != nil {
			return fmt.Errorf("decode hex signature: %w", err)
		}
		alg, err := algorithmFromPlainMedia(signed.Signature.MediaType)
		if err != nil {
			return err
		}
		return verifyRSA(alg, pubFromCreds.PublicKey, hash, dig, sig)

	case v1alpha1.MediaTypePEM:
		slog.WarnContext(ctx, "verifying signatures with PEM encoding is experimental")
		return h.verifyPEMSignature(signed, hash, dig, typedCreds)

	default:
		return fmt.Errorf("unsupported media type %q", signed.Signature.MediaType)
	}
}

// verifyPEMSignature handles the MediaTypePEM case for Verify. It parses the
// embedded chain, classifies the credential chain into intermediates and an
// optional root anchor, merges the two intermediate pools, validates the X.509
// path and issuer constraint, and finally verifies the RSA signature bytes.
func (h *Handler) verifyPEMSignature(
	signed descruntime.Signature,
	hash crypto.Hash,
	dig []byte,
	creds *rsacredentialsv1.RSACredentials,
) error {
	sig, algFromPEM, chain, err := rsasignature.GetSignatureFromPem([]byte(signed.Signature.Value))
	if err != nil {
		return fmt.Errorf("parse pem signature: %w", err)
	}
	if len(chain) == 0 {
		return errors.New("pem signature missing certificate chain")
	}
	leaf := chain[0]
	rsaPub, ok := leaf.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("leaf cert public key is not RSA")
	}

	credIntermediates, credAnchor, err := classifyCredentialChain(creds)
	if err != nil {
		return err
	}

	// Merge embedded chain intermediates with credential intermediates.
	allIntermediates := make([]*x509.Certificate, 0, len(chain)-1+len(credIntermediates))
	allIntermediates = append(allIntermediates, chain[1:]...)
	allIntermediates = append(allIntermediates, credIntermediates...)

	if err := verifyChainWithOptionalAnchor(leaf, allIntermediates, credAnchor, h.roots, h.now); err != nil {
		return fmt.Errorf("certificate verification failed: %w", err)
	}

	// Optional issuer constraint: the signature's Issuer field must match the
	// X.509 Issuer of the leaf certificate (i.e. the DN of the CA that directly
	// signed the leaf).
	if err := verifyIssuerForLeafCert(signed, leaf); err != nil {
		return fmt.Errorf("issuer verification based on leaf certificate failed: %w", err)
	}

	return verifyRSA(v1alpha1.SignatureAlgorithm(algFromPEM), rsaPub, hash, dig, sig)
}

// GetSigningCredentialConsumerIdentity requests credentials for signing.
// It encodes the algorithm and the logical signature name.
func (*Handler) GetSigningCredentialConsumerIdentity(
	_ context.Context,
	name string,
	_ descruntime.Digest,
	rawCfg runtime.Typed,
) (runtime.Identity, error) {
	var supported v1alpha1.Config
	if err := v1alpha1.Scheme.Convert(rawCfg, &supported); err != nil {
		return nil, fmt.Errorf("convert config: %w", err)
	}
	id := baseIdentity(supported.GetSignatureAlgorithm())
	id.Signature = name
	return rsaIdentityToMap(id), nil
}

// GetVerifyingCredentialConsumerIdentity requests credentials for verification.
// For plain signatures, infer algorithm from media type if empty.
// For PEM signatures, parse the PEM and ensure its algorithm matches the declared one.
// If declared is empty, use the algorithm parsed from the PEM.
func (*Handler) GetVerifyingCredentialConsumerIdentity(
	_ context.Context,
	signature descruntime.Signature,
	_ runtime.Typed,
) (runtime.Identity, error) {
	alg := signature.Signature.Algorithm

	if signature.Signature.MediaType == v1alpha1.MediaTypePEM {
		_, pemAlg, _, err := rsasignature.GetSignatureFromPem([]byte(signature.Signature.Value))
		if err != nil {
			return nil, fmt.Errorf("parse pem signature: %w", err)
		}
		if alg != "" && alg != pemAlg {
			return nil, fmt.Errorf("algorithm mismatch: declared %q, pem %q", alg, pemAlg)
		}
		if alg == "" {
			alg = pemAlg
		}
	} else if alg == "" {
		if inferred, err := algorithmFromPlainMedia(signature.Signature.MediaType); err == nil {
			alg = string(inferred)
		}
	}

	id := baseIdentity(v1alpha1.SignatureAlgorithm(alg))
	id.Signature = signature.Name
	return rsaIdentityToMap(id), nil
}

// ---- internal helpers ----

// algorithmFromPlainMedia infers the RSA algorithm from a plain media type.
func algorithmFromPlainMedia(mt string) (v1alpha1.SignatureAlgorithm, error) {
	switch mt {
	case v1alpha1.MediaTypePlainRSASSAPSS:
		return v1alpha1.AlgorithmRSASSAPSS, nil
	case v1alpha1.MediaTypePlainRSASSAPKCS1V15:
		return v1alpha1.AlgorithmRSASSAPKCS1V15, nil
	default:
		return "", fmt.Errorf("unsupported media type %q", mt)
	}
}

// isSelfSigned reports whether cert is self-signed, i.e. its Issuer equals its
// Subject and its signature can be verified with its own public key.
func isSelfSigned(cert *x509.Certificate) bool {
	return cert.CheckSignatureFrom(cert) == nil
}

// classifyCredentialChain parses the verifier-controlled credential chain and
// splits it into intermediates (non-self-signed) and an optional root anchor
// (the single self-signed cert, which must appear last if present).
// A self-signed cert at any position other than last is rejected.
func classifyCredentialChain(creds *rsacredentialsv1.RSACredentials) (intermediates []*x509.Certificate, anchor *x509.Certificate, err error) {
	chain, err := rsacredentials.CertificateChainFromCredentials(creds)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot load certificate chain from credentials: %w", err)
	}
	for i, c := range chain {
		if isSelfSigned(c) {
			if i != len(chain)-1 {
				return nil, nil, fmt.Errorf("self-signed certificate %q at position %d must be the last certificate in the credential chain", c.Subject.String(), i)
			}
			anchor = c
		} else {
			intermediates = append(intermediates, c)
		}
	}
	return intermediates, anchor, nil
}

// classifyEmbeddedChain classifies certificates from the signer-controlled
// embedded chain (the certs bundled inside the PEM signature). Self-signed
// certificates are unconditionally rejected: a signer must not embed root CAs,
// because doing so would let them assert their own trust anchor and bypass the
// verifier's credential store. All non-self-signed certs go to the intermediates
// pool for path building.
func classifyEmbeddedChain(chain []*x509.Certificate, ip *x509.CertPool) (*x509.CertPool, error) {
	for _, c := range chain {
		if isSelfSigned(c) {
			return nil, fmt.Errorf("self-signed certificate %q must not be embedded in the signature; supply root CAs via credentials instead", c.Subject.String())
		}
		if ip == nil {
			ip = x509.NewCertPool()
		}
		ip.AddCert(c)
	}
	return ip, nil
}

// verifyChainWithOptionalAnchor validates leaf against a root pool.
//
// intermediates are path-building certificates merged from the embedded
// signature chain and any credential-supplied intermediates. Self-signed
// certificates in intermediates are rejected — the signer must not embed
// root CAs, and a self-signed cert is invalid as an intermediate.
//
// anchor is the self-signed root CA from credentials, or nil:
//   - nil: system roots (h.roots) are the only trust anchors.
//   - non-nil: system roots are ignored; the chain must terminate at exactly
//     this anchor. anchor is always self-signed — non-self-signed credential
//     certs are passed as intermediates, not as the anchor.
func verifyChainWithOptionalAnchor(
	leaf *x509.Certificate,
	intermediates []*x509.Certificate,
	anchor *x509.Certificate,
	roots *x509.CertPool,
	now func() time.Time,
) error {
	if anchor != nil {
		// Credential root supplied: use an isolated pool so system roots cannot
		// satisfy the chain in place of the verifier's chosen anchor.
		roots = x509.NewCertPool()
		roots.AddCert(anchor)
	} else if roots == nil {
		roots = x509.NewCertPool()
	}

	var (
		ip  *x509.CertPool
		err error
	)

	// All intermediates come pre-merged from the call site; self-signed certs
	// are forbidden here (they must only appear as the credential anchor).
	ip, err = classifyEmbeddedChain(intermediates, ip)
	if err != nil {
		return fmt.Errorf("invalid certificate chain: %w", err)
	}

	_, err = leaf.Verify(x509.VerifyOptions{
		Intermediates: ip,
		Roots:         roots,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		CurrentTime:   now(),
	})
	return err
}

// verifyIssuerForLeafCert checks that the Issuer field declared in the signature
// matches the X.509 Issuer of the leaf certificate, i.e. the DN of the CA that
// directly signed the leaf. The check is skipped when the Issuer field is empty.
func verifyIssuerForLeafCert(signed descruntime.Signature, leaf *x509.Certificate) error {
	iss := strings.TrimSpace(signed.Signature.Issuer)
	if iss == "" {
		return nil
	}

	want, err := rfc2253.Parse(iss)
	if err != nil {
		return fmt.Errorf("parsing issuer %q failed: %w", iss, err)
	}

	leafIssuerDN := leaf.Issuer

	if err := rfc2253.Equal(want, leafIssuerDN); err != nil {
		return fmt.Errorf("issuer mismatch between %q and %q: %w", want.String(), leafIssuerDN.String(), err)
	}
	return nil
}

// baseIdentity builds a typed RSA credential consumer identity.
func baseIdentity(algorithm v1alpha1.SignatureAlgorithm) *identityv1.RSAIdentity {
	return &identityv1.RSAIdentity{
		Type:      identityv1.V1Alpha1Type,
		Algorithm: string(algorithm),
	}
}

// rsaIdentityToMap converts a typed RSAIdentity to a runtime.Identity map.
// Used at the Signer/Verifier interface boundary until Phase 4 migrates those to runtime.Typed.
func rsaIdentityToMap(id *identityv1.RSAIdentity) runtime.Identity {
	m := runtime.Identity{
		identityv1.IdentityAttributeAlgorithm: id.Algorithm,
		identityv1.IdentityAttributeSignature: id.Signature,
	}
	m.SetType(id.Type)
	return m
}
