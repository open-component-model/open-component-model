// Package tsa provides an RFC 3161 Timestamping Authority client for
// requesting and verifying timestamps on OCM component version signatures.
package tsa

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/digitorus/pkcs7"
	"github.com/digitorus/timestamp"

	"ocm.software/open-component-model/bindings/go/signing"
)

const (
	contentTypeTSQuery = "application/timestamp-query"
	pemBlockType       = "TIMESTAMP TOKEN"
	// legacyPEMBlockType is the PEM block type the legacy OCM CLI
	// (open-component-model/ocm) uses for a bare CMS SignedData timestamp.
	legacyPEMBlockType = "TIMESTAMP INFO"
)

// oidExtKeyUsage is the OID of the X.509 extended key usage extension
// (id-ce-extKeyUsage, 2.5.29.37).
var oidExtKeyUsage = asn1.ObjectIdentifier{2, 5, 29, 37}

// Token holds the result of a successful timestamp request.
type Token struct {
	// Raw is the DER-encoded TimeStampToken (a CMS ContentInfo).
	Raw []byte
	// Time is the generation time extracted from the TSTInfo.
	Time time.Time
}

// RequestTimestamp sends an RFC 3161 timestamp request to the TSA server at url.
// The hash and digest identify the data being timestamped.
// The client parameter specifies the HTTP client to use; if nil, http.DefaultClient is used.
// On success it returns a Token containing the raw DER token and its generation time.
func RequestTimestamp(ctx context.Context, client *http.Client, url string, hash crypto.Hash, digest []byte) (*Token, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if err := validateImprint(hash, digest); err != nil {
		return nil, err
	}
	redacted, err := SanitizeURL(url)
	if err != nil {
		return nil, err
	}

	nonce, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}
	reqDER, err := (&timestamp.Request{
		HashAlgorithm: hash,
		HashedMessage: digest,
		Nonce:         nonce,
		Certificates:  true,
	}).Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshaling timestamp request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqDER))
	if err != nil {
		// Do not wrap err: net/http URL errors embed the raw URL, which may
		// carry userinfo or query credentials.
		return nil, fmt.Errorf("creating HTTP request for %s failed", redacted)
	}
	httpReq.Header.Set("Content-Type", contentTypeTSQuery)

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request to %s: %w", redacted, stripURLError(err))
	}
	defer httpResp.Body.Close()

	// Limit response body to 10 MB to prevent unbounded memory allocation
	// from a malicious or misconfigured TSA server.
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("reading response from %s: %w", redacted, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server %s returned HTTP %d", redacted, httpResp.StatusCode)
	}

	// ParseResponse checks the PKI status and, because the request asked for
	// the TSA certificate, the CMS signature of the token. Trust in the TSA
	// identity is established later by Verify against configured roots.
	ts, err := timestamp.ParseResponse(body)
	if err != nil {
		return nil, fmt.Errorf("parsing timestamp response: %w", err)
	}
	if ts.HashAlgorithm != hash || !bytes.Equal(ts.HashedMessage, digest) {
		return nil, fmt.Errorf("response message imprint does not match request")
	}
	if ts.Nonce == nil || nonce.Cmp(ts.Nonce) != 0 {
		return nil, fmt.Errorf("response nonce does not match request")
	}

	return &Token{Raw: ts.RawToken, Time: ts.Time}, nil
}

// stripURLError unwraps a *url.Error to its cause. net/http formats *url.Error
// with the request URL, masking only the userinfo password, so the username and
// query (which may carry an API key) would otherwise reach error output.
func stripURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// Verify parses a DER-encoded timestamp token and verifies that:
//   - the PKCS#7 CMS signature is structurally valid
//   - the embedded message imprint matches the provided hash and digest
//   - the token has exactly one signer whose certificate carries a critical
//     RFC 3161 id-kp-timeStamping extended key usage
//
// When roots is non-nil, the signer certificate chain is additionally validated
// against roots as of the token's GenTime, requiring the timestamping EKU. In
// that case the returned trusted flag is true and the returned GenTime is safe
// to use for certificate-validation time.
//
// When roots is nil, only structural validity and the imprint/EKU checks are
// performed; the returned trusted flag is false. Callers MUST NOT use a
// non-trusted GenTime to relax X.509 certificate validity.
func Verify(raw []byte, hash crypto.Hash, digest []byte, roots *x509.CertPool) (genTime time.Time, trusted bool, err error) {
	if err := validateImprint(hash, digest); err != nil {
		return time.Time{}, false, err
	}

	// Parse checks the CMS signature only when certificates are embedded; a
	// token without them has no signer certificate and is rejected below.
	ts, err := timestamp.Parse(raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parsing timestamp token: %w", err)
	}
	if ts.HashAlgorithm != hash || !bytes.Equal(ts.HashedMessage, digest) {
		return time.Time{}, false, fmt.Errorf("timestamp message imprint does not match expected digest")
	}

	// timestamp.Parse does not expose the signer, so the token is parsed again.
	p7, err := pkcs7.Parse(raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("parsing PKCS#7 timestamp token: %w", err)
	}

	// RFC 3161 §2.3: the token MUST have a single signer whose certificate has
	// the critical id-kp-timeStamping EKU as its sole extended key usage. This
	// prevents a non-timestamping certificate chaining to a configured root from
	// fabricating a token.
	signer := p7.GetOnlySigner()
	if signer == nil {
		return time.Time{}, false, fmt.Errorf("timestamp token must have exactly one signer")
	}
	if err := verifyTimestampingEKU(signer); err != nil {
		return time.Time{}, false, err
	}

	if roots == nil {
		return ts.Time, false, nil
	}

	// GenTime (not the optional CMS signing-time attribute or the current time)
	// is the authoritative timestamp creation time per RFC 3161.
	// VerifyWithOpts does not treat the token's embedded certificates as
	// intermediates, so they are passed explicitly to complete
	// root→intermediate→TSA chains.
	intermediates := x509.NewCertPool()
	for _, cert := range p7.Certificates {
		if cert.Equal(signer) {
			continue
		}
		intermediates.AddCert(cert)
	}
	opts := x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   ts.Time,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
	}
	if err := p7.VerifyWithOpts(opts); err != nil {
		return time.Time{}, false, fmt.Errorf("verifying PKCS#7 signer certificate chain: %w", err)
	}

	return ts.Time, true, nil
}

// SignatureImprint returns the hash and imprint a TSA token over a signature
// value must cover, using the signature's declared digest hash algorithm.
func SignatureImprint(hashAlgorithm string, value []byte) (crypto.Hash, []byte, error) {
	hash, err := signing.GetSupportedHash(hashAlgorithm)
	if err != nil {
		return 0, nil, err
	}
	h := hash.New()
	h.Write(value)
	return hash, h.Sum(nil), nil
}

// validateImprint rejects hash algorithms that are not supported for signing
// and digests whose length does not match the hash.
func validateImprint(hash crypto.Hash, digest []byte) error {
	if _, err := signing.GetSupportedHash(hash.String()); err != nil {
		return err
	}
	if len(digest) != hash.Size() {
		return fmt.Errorf("digest length %d does not match %v size %d", len(digest), hash, hash.Size())
	}
	return nil
}

// verifyTimestampingEKU enforces RFC 3161 §2.3: id-kp-timeStamping must be the
// only extended key usage and its extension must be marked critical.
func verifyTimestampingEKU(cert *x509.Certificate) error {
	if len(cert.UnknownExtKeyUsage) > 0 || !slices.Equal(cert.ExtKeyUsage, []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}) {
		return fmt.Errorf("signer certificate must have id-kp-timeStamping as its only extended key usage")
	}

	// Go removes handled critical extensions from UnhandledCriticalExtensions,
	// so criticality is inspected from the raw extensions.
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oidExtKeyUsage) && !ext.Critical {
			return fmt.Errorf("signer certificate extended key usage extension is not marked critical")
		}
	}
	return nil
}

// ToPEM encodes a DER-encoded timestamp token into PEM format.
func ToPEM(raw []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{
		Type:  pemBlockType,
		Bytes: raw,
	})
}

// FromPEM decodes a PEM-encoded timestamp token back to raw DER bytes.
func FromPEM(data []byte) ([]byte, error) {
	return decodePEM(data, pemBlockType)
}

// IsLegacyPEM reports whether data holds a timestamp written by the legacy OCM
// CLI. Such timestamps cover the descriptor digest rather than the signature
// value; decode them with FromLegacyPEM.
func IsLegacyPEM(data []byte) bool {
	block, _ := pem.Decode(data)
	return block != nil && block.Type == legacyPEMBlockType
}

// FromLegacyPEM decodes a timestamp written by the legacy OCM CLI, a bare CMS
// SignedData under PEM block type "TIMESTAMP INFO", into the DER ContentInfo
// that Verify expects.
func FromLegacyPEM(data []byte) ([]byte, error) {
	signedData, err := decodePEM(data, legacyPEMBlockType)
	if err != nil {
		return nil, err
	}
	// asn1.Marshal writes a RawValue with FullBytes verbatim and ignores an
	// explicit tag, so the [0] wrapper is built here.
	der, err := asn1.Marshal(struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue
	}{
		ContentType: pkcs7.OIDSignedData,
		Content:     asn1.RawValue{Class: asn1.ClassContextSpecific, Tag: 0, IsCompound: true, Bytes: signedData},
	})
	if err != nil {
		return nil, fmt.Errorf("wrapping legacy timestamp: %w", err)
	}
	return der, nil
}

// decodePEM decodes data as a single PEM block of the given type.
func decodePEM(data []byte, blockType string) ([]byte, error) {
	block, rest := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if block.Type != blockType {
		return nil, fmt.Errorf("unexpected PEM block type %q, expected %q", block.Type, blockType)
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return nil, fmt.Errorf("trailing data after PEM block")
	}
	return block.Bytes, nil
}

// SanitizeURL returns raw with its sensitive components (userinfo, query,
// fragment) removed, preserving only scheme, host, port, and path. The result
// is safe for error messages, logs, and the signed TSA URL label: verification
// derives the credential-lookup identity from scheme/host/port/path only (see
// runtime.ParseURLToIdentity). An error is returned for URLs that cannot be
// parsed safely: a parse failure, or an opaque form (e.g. "user:pass@host/path",
// where url.Parse treats "user" as the scheme and keeps the credentials in
// URL.Opaque, which URL.String would re-emit verbatim).
func SanitizeURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parsing TSA URL failed")
	}
	if u.Opaque != "" {
		return "", fmt.Errorf("refusing opaque TSA URL that may embed credentials")
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
