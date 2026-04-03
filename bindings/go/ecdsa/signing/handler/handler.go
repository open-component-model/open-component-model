// Package handler implements ECDSA signing and verification for OCM.
// It supports NIST curves P-256, P-384, and P-521, and two encodings:
//  1. Plain: hex-encoded ASN.1 DER signature bytes without certificates.
//  2. PEM: a SIGNATURE PEM block with an embedded X.509 chain.
//
// For PEM verification, the leaf public key is taken from the chain after
// the chain validates against system roots and/or an optional trust anchor
// provided via credentials.
package handler

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ecdsacredentials "ocm.software/open-component-model/bindings/go/ecdsa/signing/handler/internal/credentials"
	ecdsasignature "ocm.software/open-component-model/bindings/go/ecdsa/signing/handler/internal/pem"
	"ocm.software/open-component-model/bindings/go/ecdsa/signing/handler/internal/rfc2253"
	"ocm.software/open-component-model/bindings/go/ecdsa/signing/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Identity attribute keys used for credential consumer identities.
const (
	IdentityAttributeAlgorithm = "algorithm"
	IdentityAttributeSignature = "signature"
)

// Common errors for callers to test.
var (
	ErrInvalidAlgorithm  = errors.New("invalid algorithm")
	ErrMissingPrivateKey = errors.New("private key not found")
	ErrMissingPublicKey  = errors.New("missing public key, required for plain ECDSA signatures")
)

// Handler holds trust anchors and time source for X.509 validation.
type Handler struct {
	roots *x509.CertPool
	now   func() time.Time
}

// New returns a Handler. If useSystemRoots is true, system trust roots are loaded.
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

// Sign produces a signature for the given digest, using ECDSA with the configured
// algorithm and encoding policy.
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

	priv, err := ecdsacredentials.PrivateKeyFromCredentials(creds)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("cannot load private key from credentials for signing: %w", err)
	}
	if priv == nil {
		return descruntime.SignatureInfo{}, ErrMissingPrivateKey
	}

	_, dig, err := parseDigest(unsigned)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}

	rawSig, err := signECDSA(algorithm, priv, dig)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("ecdsa sign: %w", err)
	}

	switch supported.GetSignatureEncodingPolicy() {
	case v1alpha1.SignatureEncodingPolicyPEM:
		slog.WarnContext(ctx, "signing with PEM encoding is experimental")
		chain, err := ecdsacredentials.CertificateChainFromCredentials(creds)
		if err != nil {
			return descruntime.SignatureInfo{}, fmt.Errorf("read certificate chain: %w", err)
		}
		pem := ecdsasignature.SignatureBytesToPem(string(algorithm), rawSig, chain...)
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
	_ runtime.Typed,
	creds map[string]string,
) error {
	pubFromCreds, err := ecdsacredentials.PublicKeyFromCredentials(creds)
	if err != nil {
		return fmt.Errorf("cannot load public key from credentials for verification: %w", err)
	}

	_, dig, err := parseDigest(signed.Digest)
	if err != nil {
		return err
	}

	switch signed.Signature.MediaType {
	case v1alpha1.MediaTypePlainECDSAP256, v1alpha1.MediaTypePlainECDSAP384, v1alpha1.MediaTypePlainECDSAP521:
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
		return verifyECDSA(alg, pubFromCreds.PublicKey, dig, sig)

	case v1alpha1.MediaTypePEM:
		slog.WarnContext(ctx, "verifying signatures with PEM encoding is experimental")
		sig, algFromPEM, chain, err := ecdsasignature.GetSignatureFromPem([]byte(signed.Signature.Value))
		if err != nil {
			return fmt.Errorf("parse pem signature: %w", err)
		}
		if len(chain) == 0 {
			return errors.New("pem signature missing certificate chain")
		}
		leaf := chain[0]
		ecdsaPub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
		if !ok {
			return errors.New("leaf cert public key is not ECDSA")
		}

		underlyingCert := pubFromCreds.GetOptionalUnderlyingCert()

		if err := verifyChainWithOptionalAnchor(leaf, chain[1:], underlyingCert, h.roots, h.now); err != nil {
			return fmt.Errorf("certificate verification failed: %w", err)
		}

		if err := verifyIssuerForUnderlyingCert(signed, underlyingCert); err != nil {
			return fmt.Errorf("issuer verification based on underlying certificate failed: %w", err)
		}

		alg, err := algorithmFromPEMAlgorithm(algFromPEM)
		if err != nil {
			return err
		}
		return verifyECDSA(alg, ecdsaPub, dig, sig)

	default:
		return fmt.Errorf("unsupported media type %q", signed.Signature.MediaType)
	}
}

// GetSigningCredentialConsumerIdentity requests credentials for signing.
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
	id[IdentityAttributeSignature] = name
	return id, nil
}

// GetVerifyingCredentialConsumerIdentity requests credentials for verification.
func (*Handler) GetVerifyingCredentialConsumerIdentity(
	_ context.Context,
	signature descruntime.Signature,
	_ runtime.Typed,
) (runtime.Identity, error) {
	alg := signature.Signature.Algorithm

	if signature.Signature.MediaType == v1alpha1.MediaTypePEM {
		_, pemAlg, _, err := ecdsasignature.GetSignatureFromPem([]byte(signature.Signature.Value))
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
	id[IdentityAttributeSignature] = signature.Name
	return id, nil
}

// algorithmFromPlainMedia infers the ECDSA algorithm from a plain media type.
func algorithmFromPlainMedia(mt string) (v1alpha1.SignatureAlgorithm, error) {
	switch mt {
	case v1alpha1.MediaTypePlainECDSAP256:
		return v1alpha1.AlgorithmECDSAP256, nil
	case v1alpha1.MediaTypePlainECDSAP384:
		return v1alpha1.AlgorithmECDSAP384, nil
	case v1alpha1.MediaTypePlainECDSAP521:
		return v1alpha1.AlgorithmECDSAP521, nil
	default:
		return "", fmt.Errorf("unsupported media type %q", mt)
	}
}

// algorithmFromPEMAlgorithm maps the PEM header algorithm string to a SignatureAlgorithm.
func algorithmFromPEMAlgorithm(pemAlg string) (v1alpha1.SignatureAlgorithm, error) {
	switch v1alpha1.SignatureAlgorithm(pemAlg) {
	case v1alpha1.AlgorithmECDSAP256:
		return v1alpha1.AlgorithmECDSAP256, nil
	case v1alpha1.AlgorithmECDSAP384:
		return v1alpha1.AlgorithmECDSAP384, nil
	case v1alpha1.AlgorithmECDSAP521:
		return v1alpha1.AlgorithmECDSAP521, nil
	default:
		return "", ErrInvalidAlgorithm
	}
}

// verifyChainWithOptionalAnchor validates leaf with intermediates against roots.
func verifyChainWithOptionalAnchor(
	leaf *x509.Certificate,
	intermediates []*x509.Certificate,
	anchor *x509.Certificate,
	roots *x509.CertPool,
	now func() time.Time,
) error {
	if roots == nil {
		roots = x509.NewCertPool()
	}
	var ip *x509.CertPool
	if len(intermediates) > 0 {
		ip = x509.NewCertPool()
		for _, c := range intermediates {
			ip.AddCert(c)
		}
	}
	if anchor != nil {
		cloned := roots.Clone()
		cloned.AddCert(anchor)
		roots = cloned
	}

	_, err := leaf.Verify(x509.VerifyOptions{
		Intermediates: ip,
		Roots:         roots,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		CurrentTime:   now(),
	})
	return err
}

// verifyIssuerForUnderlyingCert checks that the issuer matches the subject of the underlying certificate.
func verifyIssuerForUnderlyingCert(signed descruntime.Signature, underlyingCert *x509.Certificate) error {
	if underlyingCert == nil {
		return nil
	}

	iss := strings.TrimSpace(signed.Signature.Issuer)
	if iss == "" {
		return nil
	}

	want, err := rfc2253.Parse(iss)
	if err != nil {
		return fmt.Errorf("parsing issuer %q failed: %w", iss, err)
	}

	subjectDN := underlyingCert.Subject

	if err := rfc2253.Equal(want, subjectDN); err != nil {
		return fmt.Errorf("issuer mismatch between %q and %q: %w", want.String(), subjectDN.String(), err)
	}
	return nil
}

// baseIdentity builds a credential consumer identity for ECDSA handlers.
func baseIdentity(algorithm v1alpha1.SignatureAlgorithm) runtime.Identity {
	id := runtime.Identity{IdentityAttributeAlgorithm: string(algorithm)}
	id.SetType(ecdsacredentials.IdentityTypeECDSA)
	return id
}
