package tsa

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix" //nolint:staticcheck // needed for AlgorithmIdentifier in tests
	"encoding/asn1"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/digitorus/pkcs7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMessageImprint(t *testing.T) {
	digest := sha256.Sum256([]byte("hello"))

	mi, err := NewMessageImprint(crypto.SHA256, digest[:])
	require.NoError(t, err)
	assert.True(t, mi.HashAlgorithm.Algorithm.Equal(oidDigestAlgorithmSHA256))
	assert.Equal(t, digest[:], mi.HashedMessage)
}

func TestNewMessageImprint_SHA512(t *testing.T) {
	digest := sha512.Sum512([]byte("hello"))

	mi, err := NewMessageImprint(crypto.SHA512, digest[:])
	require.NoError(t, err)
	assert.True(t, mi.HashAlgorithm.Algorithm.Equal(oidDigestAlgorithmSHA512))
}

func TestNewMessageImprint_UnsupportedHash(t *testing.T) {
	_, err := NewMessageImprint(crypto.MD5, []byte("short"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported hash algorithm")
}

func TestNewMessageImprint_WrongDigestLength(t *testing.T) {
	_, err := NewMessageImprint(crypto.SHA256, []byte("too-short"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "digest length")
}

func TestMessageImprint_Hash(t *testing.T) {
	digest := sha256.Sum256([]byte("test"))
	mi, err := NewMessageImprint(crypto.SHA256, digest[:])
	require.NoError(t, err)

	h, err := mi.Hash()
	require.NoError(t, err)
	assert.Equal(t, crypto.SHA256, h)
}

func TestMessageImprint_Equal(t *testing.T) {
	digest := sha256.Sum256([]byte("hello"))
	mi1, err := NewMessageImprint(crypto.SHA256, digest[:])
	require.NoError(t, err)
	mi2, err := NewMessageImprint(crypto.SHA256, digest[:])
	require.NoError(t, err)

	assert.True(t, mi1.Equal(mi2))

	differentDigest := sha256.Sum256([]byte("world"))
	mi3, err := NewMessageImprint(crypto.SHA256, differentDigest[:])
	require.NoError(t, err)
	assert.False(t, mi1.Equal(mi3))
}

func TestMessageImprint_Equal_DifferentAlgorithm(t *testing.T) {
	digest256 := sha256.Sum256([]byte("hello"))
	mi256, err := NewMessageImprint(crypto.SHA256, digest256[:])
	require.NoError(t, err)

	digest512 := sha512.Sum512([]byte("hello"))
	mi512, err := NewMessageImprint(crypto.SHA512, digest512[:])
	require.NoError(t, err)

	assert.False(t, mi256.Equal(mi512))
}

func TestAccuracy_Duration(t *testing.T) {
	tests := []struct {
		name     string
		accuracy Accuracy
		expected time.Duration
	}{
		{
			name:     "zero",
			accuracy: Accuracy{},
			expected: 0,
		},
		{
			name:     "seconds only",
			accuracy: Accuracy{Seconds: 5},
			expected: 5 * time.Second,
		},
		{
			name:     "all fields",
			accuracy: Accuracy{Seconds: 1, Millis: 500, Micros: 100},
			expected: 1*time.Second + 500*time.Millisecond + 100*time.Microsecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.accuracy.Duration())
		})
	}
}

func TestPKIStatusInfo_Err(t *testing.T) {
	t.Run("granted", func(t *testing.T) {
		si := PKIStatusInfo{Status: StatusGranted}
		assert.NoError(t, si.Err())
	})

	t.Run("grantedWithMods", func(t *testing.T) {
		si := PKIStatusInfo{Status: StatusGrantedWithMods}
		assert.NoError(t, si.Err())
	})

	t.Run("rejection", func(t *testing.T) {
		si := PKIStatusInfo{Status: StatusRejection}
		err := si.Err()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Status(2)")
	})
}

func TestGenerateNonce(t *testing.T) {
	n1, err := GenerateNonce()
	require.NoError(t, err)
	n2, err := GenerateNonce()
	require.NoError(t, err)

	assert.NotNil(t, n1)
	assert.NotNil(t, n2)
	assert.NotEqual(t, n1, n2, "two nonces should differ")
	assert.True(t, n1.BitLen() > 0)
}

func TestPEM_RoundTrip(t *testing.T) {
	original := []byte{0x30, 0x82, 0x01, 0x00, 0xDE, 0xAD, 0xBE, 0xEF}

	encoded := ToPEM(original)
	assert.Contains(t, string(encoded), "BEGIN TIMESTAMP TOKEN")
	assert.Contains(t, string(encoded), "END TIMESTAMP TOKEN")

	decoded, err := FromPEM(encoded)
	require.NoError(t, err)
	assert.Equal(t, original, decoded)
}

func TestFromPEM_NoPEMBlock(t *testing.T) {
	_, err := FromPEM([]byte("not a pem block"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no PEM block found")
}

func TestFromPEM_WrongBlockType(t *testing.T) {
	data := []byte("-----BEGIN CERTIFICATE-----\nZm9v\n-----END CERTIFICATE-----\n")
	_, err := FromPEM(data)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected PEM block type")
}

func TestFromPEM_TrailingData(t *testing.T) {
	original := []byte{0xDE, 0xAD}
	encoded := ToPEM(original)
	encoded = append(encoded, []byte("-----BEGIN EXTRA-----\nZm9v\n-----END EXTRA-----\n")...)

	_, err := FromPEM(encoded)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trailing data")
}

// --- MessageImprint.Hash error path ---

func TestMessageImprint_Hash_UnknownOID(t *testing.T) {
	mi := MessageImprint{
		HashAlgorithm: pkix.AlgorithmIdentifier{
			Algorithm: asn1.ObjectIdentifier{1, 2, 3, 4, 5}, // unknown OID
		},
		HashedMessage: []byte("dummy"),
	}
	_, err := mi.Hash()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported digest algorithm OID")
}

// --- PKIStatusInfo.Err with FailInfo and StatusString ---

func TestPKIStatusInfo_Err_WithFailInfo(t *testing.T) {
	si := PKIStatusInfo{
		Status:   StatusRejection,
		FailInfo: asn1.BitString{Bytes: []byte{0b10100000}, BitLength: 3},
	}
	err := si.Err()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FailInfo(0b")
}

func TestPKIStatusInfo_Err_WithStatusString(t *testing.T) {
	// Encode a UTF8String "bad request" as ASN.1
	encoded, err := asn1.Marshal("bad request")
	require.NoError(t, err)

	si := PKIStatusInfo{
		Status: StatusRejection,
		StatusString: []asn1.RawValue{
			{FullBytes: encoded},
		},
	}
	err = si.Err()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "StatusString(bad request)")
}

func TestPKIStatusInfo_Err_WithStatusStringUnmarshalError(t *testing.T) {
	// Invalid ASN.1 — unmarshal should fail, parts should be empty
	si := PKIStatusInfo{
		Status: StatusRejection,
		StatusString: []asn1.RawValue{
			{FullBytes: []byte{0xFF, 0xFF}},
		},
	}
	err := si.Err()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Status(2)")
	assert.NotContains(t, err.Error(), "StatusString(")
}

func TestPKIStatusInfo_Err_WithFailInfoBits(t *testing.T) {
	// Test both 0 and 1 bits
	si := PKIStatusInfo{
		Status:   StatusRejection,
		FailInfo: asn1.BitString{Bytes: []byte{0b10101000}, BitLength: 5},
	}
	err := si.Err()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FailInfo(0b10101)")
}

// --- credentials.go tests ---

func TestTSAConsumerIdentity_Empty(t *testing.T) {
	id, err := TSAConsumerIdentity("")
	require.NoError(t, err)
	typ, err := id.ParseType()
	require.NoError(t, err)
	assert.Equal(t, "TSA", typ.Name)
	assert.Equal(t, "v1alpha1", typ.Version)
}

func TestTSAConsumerIdentity_WithURL(t *testing.T) {
	id, err := TSAConsumerIdentity("https://timestamp.example.com:8443/ts")
	require.NoError(t, err)
	typ, err := id.ParseType()
	require.NoError(t, err)
	assert.Equal(t, "TSA", typ.Name)
	assert.Equal(t, "timestamp.example.com", id["hostname"])
	assert.Equal(t, "https", id["scheme"])
	assert.Equal(t, "8443", id["port"])
	assert.Equal(t, "ts", id["path"])
}

func TestTSAConsumerIdentity_InvalidURL(t *testing.T) {
	_, err := TSAConsumerIdentity("://invalid")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing TSA URL")
}

func TestRootCertPoolFromCredentials_InlinePEM(t *testing.T) {
	_, cert := mustTSAKeyAndCert(t)
	pemData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})

	creds := map[string]string{
		CredentialKeyRootCertsPEM: string(pemData),
	}
	pool, err := RootCertPoolFromCredentials(creds)
	require.NoError(t, err)
	require.NotNil(t, pool)
}

func TestRootCertPoolFromCredentials_File(t *testing.T) {
	_, cert := mustTSAKeyAndCert(t)
	pemData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})

	path := filepath.Join(t.TempDir(), "root.pem")
	require.NoError(t, os.WriteFile(path, pemData, 0o600))

	creds := map[string]string{
		CredentialKeyRootCertsPEMFile: path,
	}
	pool, err := RootCertPoolFromCredentials(creds)
	require.NoError(t, err)
	require.NotNil(t, pool)
}

func TestRootCertPoolFromCredentials_FileNotFound(t *testing.T) {
	creds := map[string]string{
		CredentialKeyRootCertsPEMFile: "/nonexistent/path/root.pem",
	}
	_, err := RootCertPoolFromCredentials(creds)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "reading root certificates")
}

func TestRootCertPoolFromCredentials_InvalidPEM(t *testing.T) {
	creds := map[string]string{
		CredentialKeyRootCertsPEM: "not valid PEM data",
	}
	_, err := RootCertPoolFromCredentials(creds)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no valid certificates")
}

func TestRootCertPoolFromCredentials_Empty(t *testing.T) {
	pool, err := RootCertPoolFromCredentials(map[string]string{})
	require.NoError(t, err)
	assert.Nil(t, pool)
}

// --- RequestTimestamp error paths ---

func TestRequestTimestamp_BadHash(t *testing.T) {
	_, err := RequestTimestamp(t.Context(), nil, "http://example.com", crypto.MD5, []byte("x"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported hash")
}

func TestRequestTimestamp_InvalidURL(t *testing.T) {
	digest := sha256.Sum256([]byte("test"))
	_, err := RequestTimestamp(t.Context(), nil, "://bad-url", crypto.SHA256, digest[:])
	assert.Error(t, err)
}

func TestRequestTimestamp_BadResponseBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/timestamp-reply")
		w.Write([]byte("not valid asn1")) //nolint:errcheck
	}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test"))
	_, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshaling timestamp response")
}

func TestRequestTimestamp_TSARejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a valid ASN.1 response with rejection status
		type resp struct {
			Status PKIStatusInfo
		}
		respDER, _ := asn1.Marshal(resp{Status: PKIStatusInfo{Status: StatusRejection}})
		w.Header().Set("Content-Type", "application/timestamp-reply")
		w.Write(respDER) //nolint:errcheck
	}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test"))
	_, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Status(2)")
}

func TestRequestTimestamp_TrailingData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		type resp struct {
			Status PKIStatusInfo
		}
		respDER, _ := asn1.Marshal(resp{Status: PKIStatusInfo{Status: StatusGranted}})
		// Append trailing garbage
		respDER = append(respDER, 0x00, 0x00, 0x00)
		w.Header().Set("Content-Type", "application/timestamp-reply")
		w.Write(respDER) //nolint:errcheck
	}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test"))
	_, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trailing data")
}

// --- Verify error paths ---

func TestVerify_BadDER(t *testing.T) {
	digest := sha256.Sum256([]byte("test"))
	_, err := Verify([]byte("not DER"), crypto.SHA256, digest[:], nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing PKCS#7")
}

func TestVerify_BadHash(t *testing.T) {
	// Need a valid PKCS#7 to get past Parse, then fail on MessageImprint
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	require.NoError(t, err)

	// Try to verify with MD5 (unsupported)
	_, err = Verify(token.Raw, crypto.MD5, []byte("x"), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported hash")
}

// --- parseTSTInfo/parseTokenInfo error paths ---

func TestParseTSTInfo_BadDER(t *testing.T) {
	_, err := parseTSTInfo([]byte{0xFF, 0xFF})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshaling TSTInfo")
}

func TestParseTokenInfo_BadDER(t *testing.T) {
	_, err := parseTokenInfo([]byte{0xFF, 0xFF})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing PKCS#7")
}

func TestRequestTimestamp_MismatchedImprint(t *testing.T) {
	// Mock server that returns a TSTInfo with a different digest
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req Request
		asn1.Unmarshal(body, &req) //nolint:errcheck

		// Tamper: flip the digest
		tampered := make([]byte, len(req.MessageImprint.HashedMessage))
		copy(tampered, req.MessageImprint.HashedMessage)
		tampered[0] ^= 0xFF

		tstInfo := Info{
			Version:        1,
			Policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
			MessageImprint: MessageImprint{HashAlgorithm: req.MessageImprint.HashAlgorithm, HashedMessage: tampered},
			SerialNumber:   big.NewInt(1),
			GenTime:        time.Now().UTC().Truncate(time.Second),
			Nonce:          req.Nonce,
		}
		writeMockTSAResponse(t, w, tsaCert, tsaKey, tstInfo)
	}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test"))
	_, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "message imprint does not match")
}

func TestRequestTimestamp_MismatchedNonce(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req Request
		asn1.Unmarshal(body, &req) //nolint:errcheck

		tstInfo := Info{
			Version:        1,
			Policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
			MessageImprint: req.MessageImprint,
			SerialNumber:   big.NewInt(1),
			GenTime:        time.Now().UTC().Truncate(time.Second),
			Nonce:          big.NewInt(999999), // wrong nonce
		}
		writeMockTSAResponse(t, w, tsaCert, tsaKey, tstInfo)
	}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test"))
	_, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonce does not match")
}

func TestRequestTimestamp_NilNonce(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req Request
		asn1.Unmarshal(body, &req) //nolint:errcheck

		tstInfo := Info{
			Version:        1,
			Policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
			MessageImprint: req.MessageImprint,
			SerialNumber:   big.NewInt(1),
			GenTime:        time.Now().UTC().Truncate(time.Second),
			// Nonce intentionally omitted (nil)
		}
		writeMockTSAResponse(t, w, tsaCert, tsaKey, tstInfo)
	}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test"))
	_, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonce does not match")
}

func TestRequestTimestamp_BadTokenInResponse(t *testing.T) {
	// Return a valid status but garbage token
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		type resp struct {
			Status         PKIStatusInfo
			TimeStampToken asn1.RawValue `asn1:"optional"`
		}
		r2 := resp{Status: PKIStatusInfo{Status: StatusGranted}}
		r2.TimeStampToken.FullBytes = []byte{0x30, 0x03, 0x01, 0x01, 0xFF} // minimal valid ASN.1 but not PKCS#7
		r2.TimeStampToken.Class = asn1.ClassUniversal
		r2.TimeStampToken.Tag = asn1.TagSequence
		r2.TimeStampToken.IsCompound = true
		respDER, _ := asn1.Marshal(r2)
		w.Header().Set("Content-Type", "application/timestamp-reply")
		w.Write(respDER) //nolint:errcheck
	}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test"))
	_, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing TSTInfo")
}

func TestVerify_NilRoots_InvalidSignature(t *testing.T) {
	// Build a token signed by one key, but with a cert for a different key
	// This should fail p7.Verify() (the nil-roots path)
	key1, _ := mustTSAKeyAndCert(t)
	_, cert2 := mustTSAKeyAndCert(t) // different cert

	tstInfo := Info{
		Version:      1,
		Policy:       asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
		SerialNumber: big.NewInt(1),
		GenTime:      time.Now().UTC().Truncate(time.Second),
		MessageImprint: func() MessageImprint {
			d := sha256.Sum256([]byte("test"))
			mi, _ := NewMessageImprint(crypto.SHA256, d[:])
			return mi
		}(),
	}
	tstInfoDER, err := asn1.Marshal(tstInfo)
	require.NoError(t, err)

	sd, err := pkcs7.NewSignedData(tstInfoDER)
	require.NoError(t, err)
	sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
	// Sign with key1 but attach cert2 — mismatch
	require.NoError(t, sd.AddSigner(cert2, key1, pkcs7.SignerInfoConfig{}))
	p7DER, err := sd.Finish()
	require.NoError(t, err)

	digest := sha256.Sum256([]byte("test"))
	_, err = Verify(p7DER, crypto.SHA256, digest[:], nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "verifying PKCS#7 signature")
}

func TestVerify_MismatchedImprint(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("original"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	require.NoError(t, err)

	// Verify with correct hash algo but wrong digest — triggers the mismatch in Verify()
	wrongDigest := sha256.Sum256([]byte("wrong"))
	roots := x509.NewCertPool()
	roots.AddCert(tsaCert)
	_, err = Verify(token.Raw, crypto.SHA256, wrongDigest[:], roots)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestParseTSTInfo_TrailingData(t *testing.T) {
	tstInfo := Info{
		Version:      1,
		Policy:       asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
		SerialNumber: big.NewInt(1),
		GenTime:      time.Now().UTC().Truncate(time.Second),
		MessageImprint: func() MessageImprint {
			d := sha256.Sum256([]byte("test"))
			mi, _ := NewMessageImprint(crypto.SHA256, d[:])
			return mi
		}(),
	}
	der, err := asn1.Marshal(tstInfo)
	require.NoError(t, err)

	// Append trailing garbage
	der = append(der, 0x00, 0x00)
	_, err = parseTSTInfo(der)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "trailing data")
}

func TestVerify_InvalidTSTInfoContent(t *testing.T) {
	// Build a PKCS#7 SignedData whose content is NOT valid TSTInfo
	tsaKey, tsaCert := mustTSAKeyAndCert(t)

	garbageContent := []byte{0x04, 0x03, 0x66, 0x6F, 0x6F} // OCTET STRING "foo"
	sd, err := pkcs7.NewSignedData(garbageContent)
	require.NoError(t, err)
	sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
	require.NoError(t, sd.AddSigner(tsaCert, tsaKey, pkcs7.SignerInfoConfig{}))
	p7DER, err := sd.Finish()
	require.NoError(t, err)

	digest := sha256.Sum256([]byte("test"))
	_, err = Verify(p7DER, crypto.SHA256, digest[:], nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parsing TSTInfo")
}

// writeMockTSAResponse is a helper that builds a valid TSA response from a TSTInfo.
func writeMockTSAResponse(t *testing.T, w http.ResponseWriter, cert *x509.Certificate, key *rsa.PrivateKey, info Info) {
	t.Helper()

	tstInfoDER, err := asn1.Marshal(info)
	if err != nil {
		http.Error(w, "marshal tstinfo", http.StatusInternalServerError)
		return
	}

	sd, err := pkcs7.NewSignedData(tstInfoDER)
	if err != nil {
		http.Error(w, "new signed data", http.StatusInternalServerError)
		return
	}
	sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
	if err := sd.AddSigner(cert, key, pkcs7.SignerInfoConfig{}); err != nil {
		http.Error(w, "add signer", http.StatusInternalServerError)
		return
	}
	p7DER, err := sd.Finish()
	if err != nil {
		http.Error(w, "finish", http.StatusInternalServerError)
		return
	}

	type mockResp struct {
		Status         PKIStatusInfo
		TimeStampToken asn1.RawValue `asn1:"optional"`
	}
	resp := mockResp{Status: PKIStatusInfo{Status: StatusGranted}}
	resp.TimeStampToken.FullBytes = p7DER
	resp.TimeStampToken.Class = asn1.ClassUniversal
	resp.TimeStampToken.Tag = asn1.TagSequence
	resp.TimeStampToken.IsCompound = true

	respDER, err := asn1.Marshal(resp)
	if err != nil {
		http.Error(w, "marshal response", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/timestamp-reply")
	_, _ = w.Write(respDER)
}

// --- Integration tests with mock TSA server ---

func TestRequestTimestamp_MockServer(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test data"))

	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	require.NoError(t, err)
	assert.NotNil(t, token)
	assert.NotEmpty(t, token.Raw)
	assert.False(t, token.Time.IsZero())
	assert.WithinDuration(t, time.Now(), token.Time, 5*time.Second)
}

func TestRequestTimestamp_NilClient_UsesDefault(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test nil client"))

	token, err := RequestTimestamp(t.Context(), nil, server.URL, crypto.SHA256, digest[:])
	require.NoError(t, err)
	assert.NotNil(t, token)
}

func TestRequestTimestamp_AndVerify_RoundTrip(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("round trip data"))

	// Request
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	require.NoError(t, err)

	// Verify with correct root
	roots := x509.NewCertPool()
	roots.AddCert(tsaCert)
	verifiedTime, err := Verify(token.Raw, crypto.SHA256, digest[:], roots)
	require.NoError(t, err)
	assert.Equal(t, token.Time, verifiedTime)
}

func TestVerify_WrongDigest(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("original"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	require.NoError(t, err)

	// Verify with wrong digest
	wrongDigest := sha256.Sum256([]byte("tampered"))
	roots := x509.NewCertPool()
	roots.AddCert(tsaCert)
	_, err = Verify(token.Raw, crypto.SHA256, wrongDigest[:], roots)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestVerify_WrongRootCert(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("wrong root test"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	require.NoError(t, err)

	// Verify with different root cert
	_, wrongCert := mustTSAKeyAndCert(t)
	wrongRoots := x509.NewCertPool()
	wrongRoots.AddCert(wrongCert)
	_, err = Verify(token.Raw, crypto.SHA256, digest[:], wrongRoots)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "verifying PKCS#7 signature")
}

func TestVerify_NilRoots_StructuralOnly(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("structural only"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	require.NoError(t, err)

	// Verify with nil roots — structural check only
	verifiedTime, err := Verify(token.Raw, crypto.SHA256, digest[:], nil)
	require.NoError(t, err)
	assert.Equal(t, token.Time, verifiedTime)
}

func TestRequestTimestamp_PEM_RoundTrip(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("pem round trip"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	require.NoError(t, err)

	// Encode to PEM and decode back
	pemData := ToPEM(token.Raw)
	decoded, err := FromPEM(pemData)
	require.NoError(t, err)
	assert.Equal(t, token.Raw, decoded)

	// Verify the decoded token still works
	roots := x509.NewCertPool()
	roots.AddCert(tsaCert)
	verifiedTime, err := Verify(decoded, crypto.SHA256, digest[:], roots)
	require.NoError(t, err)
	assert.Equal(t, token.Time, verifiedTime)
}

func TestRequestTimestamp_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("error test"))
	_, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP 500")
}

func TestRequestTimestamp_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // slow server
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	digest := sha256.Sum256([]byte("cancel test"))
	_, err := RequestTimestamp(ctx, server.Client(), server.URL, crypto.SHA256, digest[:])
	assert.Error(t, err)
}

// --- Mock TSA server ---

func mustTSAKeyAndCert(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Test TSA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return key, cert
}

func newMockTSAHandler(t *testing.T, cert *x509.Certificate, key *rsa.PrivateKey) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}

		var req Request
		rest, err := asn1.Unmarshal(body, &req)
		if err != nil || len(rest) > 0 {
			http.Error(w, "unmarshal request", http.StatusBadRequest)
			return
		}

		tstInfo := Info{
			Version:        1,
			Policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
			MessageImprint: req.MessageImprint,
			SerialNumber:   big.NewInt(time.Now().UnixNano()),
			GenTime:        time.Now().UTC().Truncate(time.Second),
			Nonce:          req.Nonce,
		}

		tstInfoDER, err := asn1.Marshal(tstInfo)
		if err != nil {
			http.Error(w, "marshal tstinfo", http.StatusInternalServerError)
			return
		}

		sd, err := pkcs7.NewSignedData(tstInfoDER)
		if err != nil {
			http.Error(w, "new signed data", http.StatusInternalServerError)
			return
		}
		sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
		if err := sd.AddSigner(cert, key, pkcs7.SignerInfoConfig{}); err != nil {
			http.Error(w, "add signer", http.StatusInternalServerError)
			return
		}
		p7DER, err := sd.Finish()
		if err != nil {
			http.Error(w, "finish", http.StatusInternalServerError)
			return
		}

		// Build TimeStampResp {status: granted, token: p7DER}
		type mockResp struct {
			Status         PKIStatusInfo
			TimeStampToken asn1.RawValue `asn1:"optional"`
		}
		resp := mockResp{
			Status: PKIStatusInfo{Status: StatusGranted},
		}
		resp.TimeStampToken.FullBytes = p7DER
		resp.TimeStampToken.Class = asn1.ClassUniversal
		resp.TimeStampToken.Tag = asn1.TagSequence
		resp.TimeStampToken.IsCompound = true

		respDER, err := asn1.Marshal(resp)
		if err != nil {
			http.Error(w, "marshal response", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/timestamp-reply")
		_, _ = w.Write(respDER)
	})
}
