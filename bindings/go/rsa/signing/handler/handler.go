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
	ErrMissingPublicKey  = errors.New("missing public key, required for plain RSA signatures")
)

// Handler holds trust anchors and time source for X.509 validation.
type Handler struct {
	roots *x509.CertPool
	now   func() time.Time
}

// New returns a Handler. If useSystemRoots is true, system trust roots are loaded, otherwise an empty pool is used.
func New(useSystemRoots bool) (*Handler, error) {
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

// ---- SPI ----

// Sign produces a signature for the given digest, using RSA and the configured
// algorithm and encoding policy. For PEM encoding, the certificate chain is
// read from credentials and embedded into the SIGNATURE block.
func (*Handler) Sign(
	ctx context.Context,
	unsigned descruntime.Digest,
	rawCfg runtime.Typed,
	creds map[string]string,
) (descruntime.SignatureInfo, error) {
	var supported v1alpha1.Config
	if err := v1alpha1.Scheme.Convert(rawCfg, &supported); err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("convert config: %w", err)
	}
	algorithm := supported.GetSignatureAlgorithm()

	priv := rsacredentials.PrivateKeyFromCredentials(creds)
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
		chain, err := rsacredentials.CertificateChainFromCredentials(creds)
		if err != nil {
			return descruntime.SignatureInfo{}, fmt.Errorf("read certificate chain: %w", err)
		}
		pem := rsasignature.SignatureBytesToPem(algorithm, rawSig, chain...)
		return descruntime.SignatureInfo{
			Algorithm: algorithm,
			MediaType: v1alpha1.MediaTypePEM,
			Value:     string(pem),
		}, nil
	case v1alpha1.SignatureEncodingPolicyPlain:
		fallthrough
	default:
		return descruntime.SignatureInfo{
			Algorithm: algorithm,
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
	pubFromCreds := rsacredentials.PublicKeyFromCredentials(creds)

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

		underlyingCert := pubFromCreds.GetOptionalUnderlyingCert()

		if err := verifyChainWithOptionalAnchor(leaf, chain[1:], underlyingCert, h.roots, h.now); err != nil {
			return fmt.Errorf("certificate verification failed: %w", err)
		}

		// Optional issuer constraint check against the underlying certificate subject.
		if err := verifyIssuerForUnderlyingCert(signed, underlyingCert); err != nil {
			return fmt.Errorf("issuer verification based on underlying certificate failed: %w", err)
		}

		return verifyRSA(algFromPEM, rsaPub, hash, dig, sig)

	default:
		return fmt.Errorf("unsupported media type %q", signed.Signature.MediaType)
	}
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
	id[IdentityAttributeSignature] = name
	return id, nil
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
			alg = inferred
		}
	}

	id := baseIdentity(alg)
	id[IdentityAttributeSignature] = signature.Name
	return id, nil
}

// ---- internal helpers ----

// algorithmFromPlainMedia infers the RSA algorithm from a plain media type.
func algorithmFromPlainMedia(mt string) (string, error) {
	switch mt {
	case v1alpha1.MediaTypePlainRSASSAPSS:
		return v1alpha1.AlgorithmRSASSAPSS, nil
	case v1alpha1.MediaTypePlainRSASSAPKCS1V15:
		return v1alpha1.AlgorithmRSASSAPKCS1V15, nil
	default:
		return "", fmt.Errorf("unsupported media type %q", mt)
	}
}

// verifyChainWithOptionalAnchor validates leaf with intermediates against roots,
// optionally adding a provided anchor certificate to the root pool.
func verifyChainWithOptionalAnchor(
	leaf *x509.Certificate,
	intermediates []*x509.Certificate,
	anchor *x509.Certificate,
	roots *x509.CertPool,
	now func() time.Time,
) error {
	// Build empty root pool if not provided.
	if roots == nil {
		roots = x509.NewCertPool()
	}
	// Build intermediates pool if present.
	var ip *x509.CertPool
	if len(intermediates) > 0 {
		ip = x509.NewCertPool()
		for _, c := range intermediates {
			ip.AddCert(c)
		}
	}
	// Add anchor into a cloned root pool if provided.
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

// verifyIssuerForUnderlyingCert checks that the issuer of the signature matches the subject of the underlying certificate.
// If the underlying certificate is nil, this check is skipped.
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

// baseIdentity builds a credential consumer identity for RSA handlers.
func baseIdentity(algorithm string) runtime.Identity {
	id := runtime.Identity{IdentityAttributeAlgorithm: algorithm}
	id.SetType(rsacredentials.IdentityTypeRSA)
	return id
}
