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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqDER))
	if err != nil {
		return nil, fmt.Errorf("tsa: creating HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", contentTypeTSQuery)

	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("tsa: sending request to %s: %w", url, err)
	}
	defer httpResp.Body.Close()

	// Limit response body to 10 MB to prevent unbounded memory allocation
	// from a malicious or misconfigured TSA server.
	body, err := io.ReadAll(io.LimitReader(httpResp.Body, 10<<20))
	if err != nil {
		return nil, fmt.Errorf("tsa: reading response from %s: %w", url, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tsa: server %s returned HTTP %d", url, httpResp.StatusCode)
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

	info, err := parseTokenInfo(resp.TimeStampToken.FullBytes)
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
//   - the embedded message imprint matches the provided hash and digest
//   - the PKCS#7 signature is valid (if roots is non-nil, verified against roots)
//
// On success it returns the verified generation time from the TSTInfo.
func Verify(raw []byte, hash crypto.Hash, digest []byte, roots *x509.CertPool) (time.Time, error) {
	mi, err := NewMessageImprint(hash, digest)
	if err != nil {
		return time.Time{}, err
	}

	p7, err := pkcs7.Parse(raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("tsa: parsing PKCS#7 timestamp token: %w", err)
	}

	if roots != nil {
		if err := p7.VerifyWithChain(roots); err != nil {
			return time.Time{}, fmt.Errorf("tsa: verifying PKCS#7 signature: %w", err)
		}
	} else {
		if err := p7.Verify(); err != nil {
			return time.Time{}, fmt.Errorf("tsa: verifying PKCS#7 signature: %w", err)
		}
	}

	info, err := parseTSTInfo(p7.Content)
	if err != nil {
		return time.Time{}, fmt.Errorf("tsa: parsing TSTInfo: %w", err)
	}

	if !mi.Equal(info.MessageImprint) {
		return time.Time{}, fmt.Errorf("tsa: timestamp message imprint does not match expected digest")
	}

	return info.GenTime, nil
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

// parseTokenInfo extracts TSTInfo from a DER-encoded ContentInfo (the full
// TimeStampToken). It uses digitorus/pkcs7 to unwrap the CMS SignedData.
func parseTokenInfo(raw []byte) (Info, error) {
	p7, err := pkcs7.Parse(raw)
	if err != nil {
		return Info{}, fmt.Errorf("parsing PKCS#7: %w", err)
	}
	return parseTSTInfo(p7.Content)
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

