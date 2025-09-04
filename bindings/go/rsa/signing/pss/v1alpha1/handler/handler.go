// Package rsapss implements RSA-PSS signing and verification for OCM.
package rsapss

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	rsasignature "ocm.software/open-component-model/bindings/go/rsa/internal"
	rsacredentials "ocm.software/open-component-model/bindings/go/rsa/signing/pss/v1alpha1/handler/internal/credentials"
	"ocm.software/open-component-model/bindings/go/rsa/signing/pss/v1alpha1/handler/internal/dn"
	"ocm.software/open-component-model/bindings/go/rsa/signing/pss/v1alpha1/spec"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Stable identifiers and media types.
const (
	Algorithm             = spec.Type
	MediaTypePlain        = "application/vnd.ocm.signature.rsa.pss" // hex string
	MediaTypePEM          = "application/x-pem-file"                // SIGNATURE + CERTIFICATE blocks
	IdentityAttributeType = "PEM"
)

var PSSCredentialConsumerIdentity = runtime.Identity{
	runtime.IdentityAttributeType:     IdentityAttributeType,
	descruntime.IdentityAttributeName: Algorithm,
}

// PSSHandler holds trust anchors for PEM chain validation.
type PSSHandler struct {
	roots *x509.CertPool
}

// New returns a handler using system roots or an empty pool.
func New(useSystemRoots bool) (*PSSHandler, error) {
	var roots *x509.CertPool
	var err error
	if useSystemRoots {
		roots, err = x509.SystemCertPool()
		if err != nil {
			return nil, fmt.Errorf("load system roots: %w", err)
		}
	} else {
		roots = x509.NewCertPool()
	}
	return &PSSHandler{roots: roots}, nil
}

// ---- SPI ----

func (*PSSHandler) Algorithm() string { return Algorithm }

// Sign signs the provided digest per config and returns SignatureInfo.
func (*PSSHandler) Sign(
	_ context.Context,
	unsigned descruntime.Digest,
	rawCfg runtime.Typed,
	creds map[string]string,
) (descruntime.SignatureInfo, error) {
	cfg, err := decodeConfig(rawCfg)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}

	priv := rsacredentials.PrivateKeyFromCredentials(creds)
	if priv == nil {
		return descruntime.SignatureInfo{}, errors.New("private key not found")
	}

	hash, dig, err := extractHashAndDigest(unsigned)
	if err != nil {
		return descruntime.SignatureInfo{}, err
	}

	sig, err := rsa.SignPSS(rand.Reader, priv, hash, dig, nil)
	if err != nil {
		return descruntime.SignatureInfo{}, fmt.Errorf("rsa-pss sign: %w", err)
	}

	switch cfg.SignatureEncodingPolicy {
	case spec.SignatureEncodingPolicyPlain:
		return descruntime.SignatureInfo{
			Algorithm: Algorithm,
			MediaType: MediaTypePlain,
			Value:     hex.EncodeToString(sig),
		}, nil

	case spec.SignatureEncodingPolicyPEM:
		fallthrough
	default:
		chain, err := rsacredentials.CertificateChainFromCredentials(creds)
		if err != nil {
			return descruntime.SignatureInfo{}, fmt.Errorf("read certificate chain: %w", err)
		}
		pem := rsasignature.SignatureBytesToPem(Algorithm, sig, chain...)
		return descruntime.SignatureInfo{
			Algorithm: Algorithm,
			MediaType: MediaTypePEM,
			Value:     string(pem),
		}, nil
	}
}

// Verify checks the signature against the digest and optional trust inputs.
func (h *PSSHandler) Verify(
	_ context.Context,
	signed descruntime.Signature,
	_ runtime.Typed,
	creds map[string]string,
) error {
	pubFromCreds, underlying := rsacredentials.PublicKeyFromCredentials(creds)
	hash, dig, err := extractHashAndDigest(signed.Digest)
	if err != nil {
		return err
	}

	switch signed.Signature.MediaType {
	case MediaTypePlain:
		if pubFromCreds == nil {
			return errors.New("public key required for plain media type")
		}
		sig, err := hex.DecodeString(signed.Signature.Value)
		if err != nil {
			return fmt.Errorf("decode hex signature: %w", err)
		}
		return rsa.VerifyPSS(pubFromCreds, hash, dig, sig, nil)

	case MediaTypePEM:
		sig, algo, chain, err := rsasignature.GetSignatureFromPem([]byte(signed.Signature.Value))
		if err != nil {
			return fmt.Errorf("parse pem signature: %w", err)
		}
		if algo != "" && algo != Algorithm {
			return fmt.Errorf("unexpected signature algorithm %q", algo)
		}
		if len(chain) == 0 {
			return errors.New("pem signature missing certificate chain")
		}

		leaf := chain[0]
		rsaPub, ok := leaf.PublicKey.(*rsa.PublicKey)
		if !ok {
			return errors.New("leaf cert public key is not RSA")
		}

		if err := verifyChainWithOptionalAnchor(leaf, chain[1:], underlying, h.roots); err != nil {
			return fmt.Errorf("certificate verification failed: %w", err)
		}

		if iss := strings.TrimSpace(signed.Signature.Issuer); iss != "" && underlying != nil {
			want, err := dn.Parse(iss)
			if err != nil {
				return fmt.Errorf("parse issuer %q: %w", iss, err)
			}
			if uc, ok := underlying.(*x509.Certificate); ok {
				if err := dn.Match(want, uc.Subject); err != nil {
					return fmt.Errorf("issuer mismatch: %w", err)
				}
			}
		}

		return rsa.VerifyPSS(rsaPub, hash, dig, sig, nil)

	default:
		return fmt.Errorf("unsupported media type %q", signed.Signature.MediaType)
	}
}

// Identities consumed by Sign and Verify.
func (*PSSHandler) GetSigningCredentialConsumerIdentity(context.Context, runtime.Typed) (runtime.Identity, error) {
	return PSSCredentialConsumerIdentity, nil
}
func (*PSSHandler) GetVerifyingCredentialConsumerIdentity(context.Context, runtime.Typed) (runtime.Identity, error) {
	return PSSCredentialConsumerIdentity, nil
}

func decodeConfig(raw runtime.Typed) (spec.Config, error) {
	var cfg spec.Config
	if err := spec.Scheme.Convert(raw, &cfg); err != nil {
		return spec.Config{}, fmt.Errorf("convert config: %w", err)
	}
	return cfg, nil
}

func extractHashAndDigest(d descruntime.Digest) (crypto.Hash, []byte, error) {
	if d.HashAlgorithm == "" {
		return 0, nil, errors.New("missing hash algorithm")
	}
	if d.Value == "" {
		return 0, nil, errors.New("missing digest value")
	}
	b, err := hex.DecodeString(d.Value)
	if err != nil {
		return 0, nil, fmt.Errorf("invalid hex digest: %w", err)
	}
	h, err := mapHash(d.HashAlgorithm)
	if err != nil {
		return 0, nil, err
	}
	return h, b, nil
}

func mapHash(a string) (crypto.Hash, error) {
	// accept sha256/384/512 and SHA-256 style
	n := strings.ToLower(strings.ReplaceAll(a, "-", ""))
	switch n {
	case "sha256":
		return crypto.SHA256, nil
	case "sha384":
		return crypto.SHA384, nil
	case "sha512":
		return crypto.SHA512, nil
	}
	// accept exact names from crypto.Hash.String()
	switch a {
	case crypto.SHA256.String():
		return crypto.SHA256, nil
	case crypto.SHA384.String():
		return crypto.SHA384, nil
	case crypto.SHA512.String():
		return crypto.SHA512, nil
	}
	return 0, fmt.Errorf("unsupported hash algorithm %q", a)
}

func verifyChainWithOptionalAnchor(
	leaf *x509.Certificate,
	intermediates []*x509.Certificate,
	anchor any, // *x509.Certificate or nil
	roots *x509.CertPool,
) error {
	if ac, ok := anchor.(*x509.Certificate); ok {
		roots = roots.Clone()
		roots.AddCert(ac)
	}
	var ip *x509.CertPool
	if len(intermediates) > 0 {
		ip = x509.NewCertPool()
		for _, c := range intermediates {
			ip.AddCert(c)
		}
	}
	_, err := leaf.Verify(x509.VerifyOptions{
		Intermediates: ip, Roots: roots,
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		CurrentTime: leaf.NotBefore,
	})
	return err
}
