package tsa

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/digitorus/pkcs7"
)

const (
	contentTypeTSQuery = "application/timestamp-query"
	pemBlockType       = "TIMESTAMP TOKEN"
)

// Token holds the result of a successful timestamp request.
type Token struct {
	// Raw is the DER-encoded TimeStampToken (a CMS ContentInfo).
	Raw []byte
	// Time is the generation time extracted from the TSTInfo.
	Time time.Time
	// Info is the parsed TSTInfo.
	Info Info
}

// HTTPClient is the interface used by RequestTimestamp to send HTTP requests.
// It is satisfied by *http.Client.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// maxTSARedirects mirrors net/http's default redirect limit. A custom
// CheckRedirect replaces that default, so the policy must re-impose the cap.
const maxTSARedirects = 10

// RejectInsecureRedirect is an http.Client.CheckRedirect policy for TSA
// requests. The initial request URL is left unrestricted so local development
// TSA servers reachable only over plain HTTP keep working, but any redirect to a
// non-HTTPS target is refused. This blocks an HTTPS-to-HTTP downgrade in which
// net/http would forward the Authorization header (and other sensitive headers)
// derived from URL userinfo over an unencrypted connection. Setting a custom
// CheckRedirect disables net/http's built-in 10-redirect limit, so this policy
// re-imposes it to avoid an unbounded redirect loop against a hostile server.
func RejectInsecureRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxTSARedirects {
		return fmt.Errorf("tsa: stopped after %d redirects", len(via))
	}
	if req.URL.Scheme != "https" {
		return fmt.Errorf("tsa: refusing insecure redirect to %s", RedactURL(req.URL.String()))
	}
	return nil
}

// RequestTimestamp sends an RFC 3161 timestamp request to the TSA server at url.
// The hash and digest identify the data being timestamped.
// The client parameter specifies the HTTP client to use; if nil, http.DefaultClient is used.
// On success it returns a Token containing the raw DER token, the verified
// generation time, and the parsed TSTInfo.
func RequestTimestamp(ctx context.Context, client HTTPClient, url string, hash crypto.Hash, digest []byte) (*Token, error) {
	if client == nil {
		client = http.DefaultClient
	}
	mi, err := NewMessageImprint(hash, digest)
	if err != nil {
		return nil, err
	}

	nonce, err := GenerateNonce()
	if err != nil {
		return nil, err
	}

	req := Request{
		Version:        1,
		MessageImprint: mi,
		Nonce:          nonce,
		CertReq:        true,
	}

	reqDER, err := asn1.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("tsa: marshaling timestamp request: %w", err)
	}

	redacted := RedactURL(url)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqDER))
	if err != nil {
		// Do not wrap err: net/http URL errors embed the raw URL, which may
		// carry userinfo or query credentials.
		return nil, fmt.Errorf("tsa: creating HTTP request for %s failed", redacted)
	}
	httpReq.Header.Set("Content-Type", contentTypeTSQuery)

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("tsa: sending request to %s: %w", redacted, err)
	}
	defer httpResp.Body.Close()

	// Limit response body to 10 MB to prevent unbounded memory allocation
	// from a malicious or misconfigured TSA server.
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("tsa: reading response from %s: %w", redacted, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tsa: server %s returned HTTP %d", redacted, httpResp.StatusCode)
	}

	var resp Response
	rest, err := asn1.Unmarshal(body, &resp)
	if err != nil {
		return nil, fmt.Errorf("tsa: unmarshaling timestamp response: %w", err)
	}
	if len(rest) > 0 {
		return nil, fmt.Errorf("tsa: trailing data in timestamp response")
	}

	if err := resp.Status.Err(); err != nil {
		return nil, err
	}

	// Cryptographically validate the returned TimeStampToken before trusting it.
	// CertReq:true above requests the TSA to embed its signing certificate, so the
	// CMS SignedData is self-contained and can be structurally verified here.
	// Trust-anchor (root CA) verification happens later at Verify() time using the
	// verifier-controlled credential graph.
	p7, err := pkcs7.Parse(resp.TimeStampToken.FullBytes)
	if err != nil {
		return nil, fmt.Errorf("tsa: parsing PKCS#7 timestamp token: %w", err)
	}
	if err := p7.Verify(); err != nil {
		return nil, fmt.Errorf("tsa: verifying PKCS#7 timestamp token signature: %w", err)
	}

	info, err := parseTSTInfo(p7.Content)
	if err != nil {
		return nil, fmt.Errorf("tsa: parsing TSTInfo from response: %w", err)
	}

	if !mi.Equal(info.MessageImprint) {
		return nil, fmt.Errorf("tsa: response message imprint does not match request")
	}
	if info.Nonce == nil || nonce.Cmp(info.Nonce) != 0 {
		return nil, fmt.Errorf("tsa: response nonce does not match request")
	}

	return &Token{
		Raw:  resp.TimeStampToken.FullBytes,
		Time: info.GenTime,
		Info: info,
	}, nil
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
	mi, err := NewMessageImprint(hash, digest)
	if err != nil {
		return time.Time{}, false, err
	}

	p7, err := pkcs7.Parse(raw)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("tsa: parsing PKCS#7 timestamp token: %w", err)
	}

	// Structurally verify the CMS signature over the token content. This does
	// not establish trust in the TSA identity; chain validation below does.
	if err := p7.Verify(); err != nil {
		return time.Time{}, false, fmt.Errorf("tsa: verifying PKCS#7 signature: %w", err)
	}

	info, err := parseTSTInfo(p7.Content)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("tsa: parsing TSTInfo: %w", err)
	}

	if !mi.Equal(info.MessageImprint) {
		return time.Time{}, false, fmt.Errorf("tsa: timestamp message imprint does not match expected digest")
	}

	// RFC 3161 §2.3: the token MUST have a single signer whose certificate has
	// the critical id-kp-timeStamping EKU as its sole extended key usage. This
	// prevents a non-timestamping certificate chaining to a configured root from
	// fabricating a token.
	signer := p7.GetOnlySigner()
	if signer == nil {
		return time.Time{}, false, fmt.Errorf("tsa: timestamp token must have exactly one signer")
	}
	if err := verifyTimestampingEKU(signer); err != nil {
		return time.Time{}, false, err
	}

	if roots == nil {
		return info.GenTime, false, nil
	}

	// Validate the signer chain against the trusted roots as of GenTime, again
	// requiring the timestamping EKU across the chain. GenTime (not the optional
	// CMS signing-time attribute or the current time) is the authoritative
	// timestamp creation time per RFC 3161.
	//
	// VerifyWithOpts does not treat the token's embedded certificates as
	// intermediates, so a root→intermediate→TSA chain would fail even with the
	// correct root trusted. Feed every embedded certificate except the signer
	// leaf as an intermediate to complete such chains.
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
		CurrentTime:   info.GenTime,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
	}
	if err := p7.VerifyWithOpts(opts); err != nil {
		return time.Time{}, false, fmt.Errorf("tsa: verifying PKCS#7 signer certificate chain: %w", err)
	}

	return info.GenTime, true, nil
}

// verifyTimestampingEKU enforces the RFC 3161 §2.3 requirement that a TSA signer
// certificate carries the id-kp-timeStamping extended key usage, that it is the
// only extended key usage present, and that the EKU extension is marked critical.
func verifyTimestampingEKU(cert *x509.Certificate) error {
	hasTimeStamping := false
	for _, eku := range cert.ExtKeyUsage {
		if eku == x509.ExtKeyUsageTimeStamping {
			hasTimeStamping = true
			continue
		}
		return fmt.Errorf("tsa: signer certificate has a non-timestamping extended key usage")
	}
	if len(cert.UnknownExtKeyUsage) > 0 {
		return fmt.Errorf("tsa: signer certificate has additional unrecognized extended key usages")
	}
	if !hasTimeStamping {
		return fmt.Errorf("tsa: signer certificate lacks the id-kp-timeStamping extended key usage")
	}

	// RFC 3161 requires the EKU extension to be critical. Go removes handled
	// critical extensions from UnhandledCriticalExtensions, so criticality is
	// inspected from the raw extensions.
	for _, ext := range cert.Extensions {
		if ext.Id.Equal(oidExtKeyUsage) {
			if !ext.Critical {
				return fmt.Errorf("tsa: signer certificate extended key usage extension is not marked critical")
			}
			return nil
		}
	}
	return fmt.Errorf("tsa: signer certificate is missing the extended key usage extension")
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
	block, rest := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("tsa: no PEM block found")
	}
	if block.Type != pemBlockType {
		return nil, fmt.Errorf("tsa: unexpected PEM block type %q, expected %q", block.Type, pemBlockType)
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return nil, fmt.Errorf("tsa: trailing data after PEM block")
	}
	return block.Bytes, nil
}

// parseTSTInfo unmarshals TSTInfo from DER-encoded bytes (the eContent
// of the CMS EncapsulatedContentInfo).
func parseTSTInfo(der []byte) (Info, error) {
	var info Info
	rest, err := asn1.Unmarshal(der, &info)
	if err != nil {
		return Info{}, fmt.Errorf("unmarshaling TSTInfo: %w", err)
	}
	if len(rest) > 0 {
		return Info{}, fmt.Errorf("trailing data in TSTInfo")
	}
	return info, nil
}

// RedactURL strips potentially sensitive components (userinfo, query, fragment)
// from a URL so it can be safely included in error messages and logs. A fixed
// placeholder is returned when the URL cannot be parsed, or when it parses to an
// opaque form (e.g. "user:pass@host/path", where url.Parse treats "user" as the
// scheme and keeps "pass@host/path" in URL.Opaque): in that case URL.User is nil,
// so URL.String would otherwise re-emit the credential-bearing input verbatim.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return "<invalid TSA URL>"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// SanitizeURL returns raw with its sensitive components (userinfo, query,
// fragment) removed, preserving only scheme, host, port, and path. It is used to
// store the TSA URL as a signed descriptor label: verification derives the
// credential-lookup identity from scheme/host/port/path only (see
// runtime.ParseURLToIdentity), so the stripped components are never needed and
// must not be persisted where anyone reading the component version could see
// them. An error is returned for URLs that cannot be parsed safely (parse
// failure or an opaque form that may still embed credentials).
func SanitizeURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("tsa: parsing TSA URL for sanitization failed")
	}
	if u.Opaque != "" {
		return "", fmt.Errorf("tsa: refusing to store opaque TSA URL that may embed credentials")
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
