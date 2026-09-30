package tsa

import (
	"bytes"
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
	"github.com/stretchr/testify/require"

	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	tsacredentialsv1alpha1 "ocm.software/open-component-model/bindings/go/signing/tsa/spec/credentials/v1alpha1"
)

func TestNewMessageImprint(t *testing.T) {
	sha256Digest := sha256.Sum256([]byte("hello"))
	sha512Digest := sha512.Sum512([]byte("hello"))
	tests := []struct {
		name    string
		hash    crypto.Hash
		digest  []byte
		wantOID asn1.ObjectIdentifier
		wantErr string
	}{
		{name: "SHA-256", hash: crypto.SHA256, digest: sha256Digest[:], wantOID: oidDigestAlgorithmSHA256},
		{name: "SHA-512", hash: crypto.SHA512, digest: sha512Digest[:], wantOID: oidDigestAlgorithmSHA512},
		{name: "unsupported hash", hash: crypto.MD5, digest: []byte("short"), wantErr: "unsupported hash algorithm"},
		{name: "wrong digest length", hash: crypto.SHA256, digest: []byte("too-short"), wantErr: "digest length"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			mi, err := NewMessageImprint(tc.hash, tc.digest)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.True(mi.HashAlgorithm.Algorithm.Equal(tc.wantOID))
			r.Equal(tc.digest, mi.HashedMessage)
		})
	}
}

func TestMessageImprint_Hash(t *testing.T) {
	digest := sha256.Sum256([]byte("test"))
	known, err := NewMessageImprint(crypto.SHA256, digest[:])
	require.NoError(t, err)
	unknown := MessageImprint{
		HashAlgorithm: pkix.AlgorithmIdentifier{Algorithm: asn1.ObjectIdentifier{1, 2, 3, 4, 5}},
		HashedMessage: []byte("dummy"),
	}

	tests := []struct {
		name    string
		imprint MessageImprint
		want    crypto.Hash
		wantErr string
	}{
		{name: "known algorithm", imprint: known, want: crypto.SHA256},
		{name: "unknown algorithm", imprint: unknown, wantErr: "unsupported digest algorithm OID"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			h, err := tc.imprint.Hash()
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, h)
		})
	}
}

func TestMessageImprint_Equal(t *testing.T) {
	r := require.New(t)
	digest := sha256.Sum256([]byte("hello"))
	mi1, err := NewMessageImprint(crypto.SHA256, digest[:])
	r.NoError(err)
	mi2, err := NewMessageImprint(crypto.SHA256, digest[:])
	r.NoError(err)

	r.True(mi1.Equal(mi2))

	differentDigest := sha256.Sum256([]byte("world"))
	mi3, err := NewMessageImprint(crypto.SHA256, differentDigest[:])
	r.NoError(err)
	r.False(mi1.Equal(mi3))
}

func TestMessageImprint_Equal_DifferentAlgorithm(t *testing.T) {
	r := require.New(t)
	digest256 := sha256.Sum256([]byte("hello"))
	mi256, err := NewMessageImprint(crypto.SHA256, digest256[:])
	r.NoError(err)

	digest512 := sha512.Sum512([]byte("hello"))
	mi512, err := NewMessageImprint(crypto.SHA512, digest512[:])
	r.NoError(err)

	r.False(mi256.Equal(mi512))
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
			r := require.New(t)
			r.Equal(tt.expected, tt.accuracy.Duration())
		})
	}
}

func TestPKIStatusInfo_Err(t *testing.T) {
	statusString, err := asn1.Marshal("bad request")
	require.NoError(t, err)
	tests := []struct {
		name        string
		info        PKIStatusInfo
		wantErr     []string
		wantMissing string
	}{
		{name: "granted", info: PKIStatusInfo{Status: StatusGranted}},
		{name: "granted with modifications", info: PKIStatusInfo{Status: StatusGrantedWithMods}},
		{name: "rejection", info: PKIStatusInfo{Status: StatusRejection}, wantErr: []string{"Status(2)"}},
		{
			name:    "fail info bits",
			info:    PKIStatusInfo{Status: StatusRejection, FailInfo: asn1.BitString{Bytes: []byte{0b10101000}, BitLength: 5}},
			wantErr: []string{"FailInfo(0b10101)"},
		},
		{
			name:    "status string",
			info:    PKIStatusInfo{Status: StatusRejection, StatusString: []asn1.RawValue{{FullBytes: statusString}}},
			wantErr: []string{"StatusString(bad request)"},
		},
		{
			name:        "unparsable status string is omitted",
			info:        PKIStatusInfo{Status: StatusRejection, StatusString: []asn1.RawValue{{FullBytes: []byte{0xFF, 0xFF}}}},
			wantErr:     []string{"Status(2)"},
			wantMissing: "StatusString(",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			err := tc.info.Err()
			if len(tc.wantErr) == 0 {
				r.NoError(err)
				return
			}
			r.Error(err)
			for _, want := range tc.wantErr {
				r.Contains(err.Error(), want)
			}
			if tc.wantMissing != "" {
				r.NotContains(err.Error(), tc.wantMissing)
			}
		})
	}
}

func TestGenerateNonce(t *testing.T) {
	r := require.New(t)
	n1, err := GenerateNonce()
	r.NoError(err)
	n2, err := GenerateNonce()
	r.NoError(err)

	r.NotNil(n1)
	r.NotNil(n2)
	r.NotEqual(n1, n2, "two nonces should differ")
	r.True(n1.BitLen() > 0)
}

func TestPEM_RoundTrip(t *testing.T) {
	r := require.New(t)
	original := []byte{0x30, 0x82, 0x01, 0x00, 0xDE, 0xAD, 0xBE, 0xEF}

	encoded := ToPEM(original)
	r.Contains(string(encoded), "BEGIN TIMESTAMP TOKEN")
	r.Contains(string(encoded), "END TIMESTAMP TOKEN")

	decoded, err := FromPEM(encoded)
	r.NoError(err)
	r.Equal(original, decoded)
}

func TestFromPEM_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{name: "no PEM block", data: []byte("not a pem block"), wantErr: "no PEM block found"},
		{name: "wrong block type", data: []byte("-----BEGIN CERTIFICATE-----\nZm9v\n-----END CERTIFICATE-----\n"), wantErr: "unexpected PEM block type"},
		{name: "trailing data", data: append(ToPEM([]byte{0xDE, 0xAD}), []byte("-----BEGIN EXTRA-----\nZm9v\n-----END EXTRA-----\n")...), wantErr: "trailing data"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromPEM(tc.data)
			require.New(t).ErrorContains(err, tc.wantErr)
		})
	}
}

func TestIsLegacyPEM(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{name: "legacy OCM block", data: pem.EncodeToMemory(&pem.Block{Type: "TIMESTAMP INFO", Bytes: []byte{0x30, 0x00}}), want: true},
		{name: "timestamp token block", data: ToPEM([]byte{0x30, 0x00})},
		{name: "not PEM", data: []byte("garbage")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.New(t).Equal(tc.want, IsLegacyPEM(tc.data))
		})
	}
}

func TestFromLegacyPEM_RoundTripVerifies(t *testing.T) {
	r := require.New(t)
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)
	digest := sha256.Sum256([]byte("descriptor digest"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	r.NoError(err)

	// The legacy OCM CLI stores the bare SignedData, without the ContentInfo wrapper.
	var contentInfo struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	_, err = asn1.Unmarshal(token.Raw, &contentInfo)
	r.NoError(err)
	legacy := pem.EncodeToMemory(&pem.Block{Type: "TIMESTAMP INFO", Bytes: contentInfo.Content.Bytes})

	der, err := FromLegacyPEM(legacy)
	r.NoError(err)

	roots := x509.NewCertPool()
	roots.AddCert(tsaCert)
	_, trusted, err := Verify(der, crypto.SHA256, digest[:], roots)
	r.NoError(err)
	r.True(trusted)
}

func TestFromLegacyPEM_Rejects(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{name: "not PEM", data: []byte("garbage")},
		{name: "current token block", data: ToPEM([]byte{0x30, 0x00})},
		{name: "trailing data", data: append(pem.EncodeToMemory(&pem.Block{Type: "TIMESTAMP INFO", Bytes: []byte{0x30, 0x00}}), []byte("extra")...)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := FromLegacyPEM(tc.data)
			require.New(t).Error(err)
		})
	}
}

// --- MessageImprint.Hash error path ---

// --- PKIStatusInfo.Err with FailInfo and StatusString ---

// --- credentials.go tests ---

func TestTSAConsumerIdentity(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    map[string]string
		wantErr string
	}{
		{name: "generic", want: map[string]string{"type": "TSA"}},
		{
			name: "from URL",
			url:  "https://timestamp.example.com:8443/ts",
			want: map[string]string{"type": "TSA", "hostname": "timestamp.example.com", "scheme": "https", "port": "8443", "path": "ts"},
		},
		{name: "invalid URL", url: "://invalid", wantErr: "parsing TSA URL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			id, err := TSAConsumerIdentity(tc.url)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, map[string]string(id))
		})
	}
}

func directCreds(props map[string]string) *credconfigv1.DirectCredentials {
	return &credconfigv1.DirectCredentials{
		Type:       runtime.NewVersionedType(credconfigv1.CredentialsType, credconfigv1.Version),
		Properties: props,
	}
}

func TestRootCertPool(t *testing.T) {
	_, cert := mustTSAKeyAndCert(t)
	pemData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	path := filepath.Join(t.TempDir(), "root.pem")
	require.NoError(t, os.WriteFile(path, pemData, 0o600))

	tests := []struct {
		name     string
		creds    *tsacredentialsv1alpha1.TSACredentials
		wantPool bool
		wantErr  string
	}{
		{name: "inline PEM", creds: &tsacredentialsv1alpha1.TSACredentials{RootCertsPEM: string(pemData)}, wantPool: true},
		{name: "PEM file", creds: &tsacredentialsv1alpha1.TSACredentials{RootCertsPEMFile: path}, wantPool: true},
		{name: "missing file", creds: &tsacredentialsv1alpha1.TSACredentials{RootCertsPEMFile: "/nonexistent/path/root.pem"}, wantErr: "reading root certificates"},
		{name: "invalid PEM", creds: &tsacredentialsv1alpha1.TSACredentials{RootCertsPEM: "not valid PEM data"}, wantErr: "no valid certificates"},
		{name: "no credentials"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			if tc.creds != nil {
				tc.creds.Type = tsacredentialsv1alpha1.VersionedType
			}
			pool, err := RootCertPool(tc.creds)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantPool, pool != nil)
		})
	}
}

// RootCertPoolFromCredentials converts resolved credentials (typed or the
// untyped DirectCredentials fallback) before loading the pool. The fallback
// still accepts the deprecated snake_case keys used by existing .ocmconfig files.
func TestRootCertPoolFromCredentials(t *testing.T) {
	_, cert := mustTSAKeyAndCert(t)
	pemData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
	path := filepath.Join(t.TempDir(), "root.pem")
	require.NoError(t, os.WriteFile(path, pemData, 0o600))

	tests := []struct {
		name     string
		creds    runtime.Typed
		wantPool bool
	}{
		{name: "direct credentials, camelCase", creds: directCreds(map[string]string{"rootCertsPEM": string(pemData)}), wantPool: true},
		{name: "direct credentials, deprecated snake_case", creds: directCreds(map[string]string{"root_certs_pem_file": path}), wantPool: true},
		{name: "no credentials"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			pool, err := RootCertPoolFromCredentials(tc.creds)
			r.NoError(err)
			r.Equal(tc.wantPool, pool != nil)
		})
	}
}

// --- RequestTimestamp error paths ---

func TestRequestTimestamp_RejectsInvalidInput(t *testing.T) {
	digest := sha256.Sum256([]byte("test"))
	tests := []struct {
		name    string
		url     string
		hash    crypto.Hash
		digest  []byte
		wantErr string
	}{
		{name: "unsupported hash", url: "http://example.com", hash: crypto.MD5, digest: []byte("x"), wantErr: "unsupported hash"},
		{name: "invalid URL", url: "://bad-url", hash: crypto.SHA256, digest: digest[:], wantErr: "creating HTTP request"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := RequestTimestamp(t.Context(), nil, tc.url, tc.hash, tc.digest)
			require.New(t).ErrorContains(err, tc.wantErr)
		})
	}
}

func TestRequestTimestamp_TransportErrorRedactsURL(t *testing.T) {
	r := require.New(t)
	digest := sha256.Sum256([]byte("test"))
	// Port 1 on loopback is closed, so the dial fails without external traffic.
	_, err := RequestTimestamp(t.Context(), &http.Client{}, "http://alice@127.0.0.1:1/tsa?apikey=S3CR3T", crypto.SHA256, digest[:])
	r.Error(err)
	r.NotContains(err.Error(), "S3CR3T")
	r.NotContains(err.Error(), "alice")
	r.Contains(err.Error(), "http://127.0.0.1:1/tsa")
}

func TestRequestTimestamp_RejectsInvalidResponses(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	type statusOnly struct {
		Status PKIStatusInfo
	}
	writeDER := func(w http.ResponseWriter, der []byte) {
		w.Header().Set("Content-Type", "application/timestamp-reply")
		_, _ = w.Write(der)
	}
	// respondWithInfo answers with a signed token whose TSTInfo is derived from
	// the request and then altered by mutate.
	respondWithInfo := func(mutate func(req Request, info *Info)) http.HandlerFunc {
		return func(w http.ResponseWriter, httpReq *http.Request) {
			body, err := io.ReadAll(httpReq.Body)
			if err != nil {
				http.Error(w, "read body", http.StatusBadRequest)
				return
			}
			var req Request
			if _, err := asn1.Unmarshal(body, &req); err != nil {
				http.Error(w, "unmarshal request", http.StatusBadRequest)
				return
			}
			info := Info{
				Version:        1,
				Policy:         asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0},
				MessageImprint: req.MessageImprint,
				SerialNumber:   big.NewInt(1),
				GenTime:        time.Now().UTC().Truncate(time.Second),
				Nonce:          req.Nonce,
			}
			mutate(req, &info)
			writeMockTSAResponse(t, w, tsaCert, tsaKey, info)
		}
	}

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "internal error", http.StatusInternalServerError)
			},
			wantErr: "HTTP 500",
		},
		{
			name:    "malformed response",
			handler: func(w http.ResponseWriter, _ *http.Request) { writeDER(w, []byte("not valid asn1")) },
			wantErr: "unmarshaling timestamp response",
		},
		{
			name: "rejection status",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				der, _ := asn1.Marshal(statusOnly{Status: PKIStatusInfo{Status: StatusRejection}})
				writeDER(w, der)
			},
			wantErr: "Status(2)",
		},
		{
			name: "trailing data",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				der, _ := asn1.Marshal(statusOnly{Status: PKIStatusInfo{Status: StatusGranted}})
				writeDER(w, append(der, 0x00, 0x00, 0x00))
			},
			wantErr: "trailing data",
		},
		{
			name: "token is not PKCS#7",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				der, _ := asn1.Marshal(struct {
					Status         PKIStatusInfo
					TimeStampToken asn1.RawValue `asn1:"optional"`
				}{
					Status:         PKIStatusInfo{Status: StatusGranted},
					TimeStampToken: asn1.RawValue{FullBytes: []byte{0x30, 0x03, 0x01, 0x01, 0xFF}},
				})
				writeDER(w, der)
			},
			wantErr: "parsing PKCS#7 timestamp token",
		},
		{
			name: "mismatched imprint",
			handler: respondWithInfo(func(req Request, info *Info) {
				tampered := bytes.Clone(req.MessageImprint.HashedMessage)
				tampered[0] ^= 0xFF
				info.MessageImprint.HashedMessage = tampered
			}),
			wantErr: "message imprint does not match",
		},
		{
			name:    "mismatched nonce",
			handler: respondWithInfo(func(_ Request, info *Info) { info.Nonce = big.NewInt(999999) }),
			wantErr: "nonce does not match",
		},
		{
			name:    "missing nonce",
			handler: respondWithInfo(func(_ Request, info *Info) { info.Nonce = nil }),
			wantErr: "nonce does not match",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			server := httptest.NewServer(tc.handler)
			t.Cleanup(server.Close)

			digest := sha256.Sum256([]byte("test"))
			_, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
			r.ErrorContains(err, tc.wantErr)
		})
	}
}

// --- Verify error paths ---

func TestVerify(t *testing.T) {
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("original"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	require.NoError(t, err)

	roots := x509.NewCertPool()
	roots.AddCert(tsaCert)
	_, otherCert := mustTSAKeyAndCert(t)
	otherRoots := x509.NewCertPool()
	otherRoots.AddCert(otherCert)
	wrongDigest := sha256.Sum256([]byte("tampered"))

	tests := []struct {
		name        string
		raw         []byte
		hash        crypto.Hash
		digest      []byte
		roots       *x509.CertPool
		wantTrusted bool
		wantErr     string
	}{
		{name: "trusted with configured root", raw: token.Raw, hash: crypto.SHA256, digest: digest[:], roots: roots, wantTrusted: true},
		{name: "structural only without roots", raw: token.Raw, hash: crypto.SHA256, digest: digest[:]},
		{name: "not DER", raw: []byte("not DER"), hash: crypto.SHA256, digest: digest[:], wantErr: "parsing PKCS#7"},
		{name: "unsupported hash", raw: token.Raw, hash: crypto.MD5, digest: []byte("x"), wantErr: "unsupported hash"},
		{name: "wrong digest", raw: token.Raw, hash: crypto.SHA256, digest: wrongDigest[:], roots: roots, wantErr: "does not match"},
		{name: "untrusted root", raw: token.Raw, hash: crypto.SHA256, digest: digest[:], roots: otherRoots, wantErr: "verifying PKCS#7 signer certificate chain"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			verifiedTime, trusted, err := Verify(tc.raw, tc.hash, tc.digest, tc.roots)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantTrusted, trusted)
			r.Equal(token.Time, verifiedTime)
		})
	}
}

// --- parseTSTInfo error paths ---

func TestParseTSTInfo_BadDER(t *testing.T) {
	r := require.New(t)
	_, err := parseTSTInfo([]byte{0xFF, 0xFF})
	r.Error(err)
	r.Contains(err.Error(), "unmarshaling TSTInfo")
}

func TestVerify_NilRoots_InvalidSignature(t *testing.T) {
	r := require.New(t)
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
	r.NoError(err)

	sd, err := pkcs7.NewSignedData(tstInfoDER)
	r.NoError(err)
	sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
	// Sign with key1 but attach cert2 — mismatch
	r.NoError(sd.AddSigner(cert2, key1, pkcs7.SignerInfoConfig{}))
	p7DER, err := sd.Finish()
	r.NoError(err)

	digest := sha256.Sum256([]byte("test"))
	_, _, err = Verify(p7DER, crypto.SHA256, digest[:], nil)
	r.Error(err)
	r.Contains(err.Error(), "verifying PKCS#7 signature")
}

func TestVerify_MismatchedImprint(t *testing.T) {
	r := require.New(t)
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("original"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	r.NoError(err)

	// Verify with correct hash algo but wrong digest — triggers the mismatch in Verify()
	wrongDigest := sha256.Sum256([]byte("wrong"))
	roots := x509.NewCertPool()
	roots.AddCert(tsaCert)
	_, _, err = Verify(token.Raw, crypto.SHA256, wrongDigest[:], roots)
	r.Error(err)
	r.Contains(err.Error(), "does not match")
}

func TestParseTSTInfo_TrailingData(t *testing.T) {
	r := require.New(t)
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
	r.NoError(err)

	// Append trailing garbage
	der = append(der, 0x00, 0x00)
	_, err = parseTSTInfo(der)
	r.Error(err)
	r.Contains(err.Error(), "trailing data")
}

func TestVerify_InvalidTSTInfoContent(t *testing.T) {
	r := require.New(t)
	// Build a PKCS#7 SignedData whose content is NOT valid TSTInfo
	tsaKey, tsaCert := mustTSAKeyAndCert(t)

	garbageContent := []byte{0x04, 0x03, 0x66, 0x6F, 0x6F} // OCTET STRING "foo"
	sd, err := pkcs7.NewSignedData(garbageContent)
	r.NoError(err)
	sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
	r.NoError(sd.AddSigner(tsaCert, tsaKey, pkcs7.SignerInfoConfig{}))
	p7DER, err := sd.Finish()
	r.NoError(err)

	digest := sha256.Sum256([]byte("test"))
	_, _, err = Verify(p7DER, crypto.SHA256, digest[:], nil)
	r.Error(err)
	r.Contains(err.Error(), "parsing TSTInfo")
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
	r := require.New(t)
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test data"))

	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	r.NoError(err)
	r.NotNil(token)
	r.NotEmpty(token.Raw)
	r.False(token.Time.IsZero())
	r.WithinDuration(time.Now(), token.Time, 5*time.Second)
}

func TestRequestTimestamp_NilClient_UsesDefault(t *testing.T) {
	r := require.New(t)
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("test nil client"))

	token, err := RequestTimestamp(t.Context(), nil, server.URL, crypto.SHA256, digest[:])
	r.NoError(err)
	r.NotNil(token)
}

func TestVerify_RejectsNonTimestampingEKU(t *testing.T) {
	r := require.New(t)
	// Build a cert with ServerAuth EKU (critical) instead of TimeStamping.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	r.NoError(err)

	// Marshal a critical EKU extension containing only id-kp-serverAuth (1.3.6.1.5.5.7.3.1).
	ekuVal, err := asn1.Marshal([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 1}})
	r.NoError(err)

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Test Non-TSA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		ExtraExtensions: []pkix.Extension{{
			Id:       asn1.ObjectIdentifier{2, 5, 29, 37}, // id-ce-extKeyUsage
			Critical: true,
			Value:    ekuVal,
		}},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)
	badCert, err := x509.ParseCertificate(der)
	r.NoError(err)

	// Build a mock TSA server using this non-timestamping cert.
	server := httptest.NewServer(newMockTSAHandler(t, badCert, key))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("eku rejection test"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	r.NoError(err)

	roots := x509.NewCertPool()
	roots.AddCert(badCert)
	_, _, err = Verify(token.Raw, crypto.SHA256, digest[:], roots)
	r.Error(err)
	r.Contains(err.Error(), "non-timestamping")
}

func TestVerify_RejectsNonCriticalEKU(t *testing.T) {
	r := require.New(t)
	// Build a cert with TimeStamping EKU that is NOT marked critical.
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	r.NoError(err)

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Test Non-Critical EKU"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping},
		BasicConstraintsValid: true,
		IsCA:                  true,
		// Go's standard x509.CreateCertificate emits a NON-critical EKU extension
		// when using the ExtKeyUsage field. This is exactly what Verify must reject.
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)
	badCert, err := x509.ParseCertificate(der)
	r.NoError(err)

	server := httptest.NewServer(newMockTSAHandler(t, badCert, key))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("non-critical eku test"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	r.NoError(err)

	roots := x509.NewCertPool()
	roots.AddCert(badCert)
	_, _, err = Verify(token.Raw, crypto.SHA256, digest[:], roots)
	r.Error(err)
	r.Contains(err.Error(), "not marked critical")
}

func TestRequestTimestamp_PEM_RoundTrip(t *testing.T) {
	r := require.New(t)
	tsaKey, tsaCert := mustTSAKeyAndCert(t)
	server := httptest.NewServer(newMockTSAHandler(t, tsaCert, tsaKey))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("pem round trip"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	r.NoError(err)

	// Encode to PEM and decode back
	pemData := ToPEM(token.Raw)
	decoded, err := FromPEM(pemData)
	r.NoError(err)
	r.Equal(token.Raw, decoded)

	// Verify the decoded token still works
	roots := x509.NewCertPool()
	roots.AddCert(tsaCert)
	verifiedTime, trusted, err := Verify(decoded, crypto.SHA256, digest[:], roots)
	r.NoError(err)
	r.True(trusted)
	r.Equal(token.Time, verifiedTime)
}

func TestRequestTimestamp_ContextCancelled(t *testing.T) {
	r := require.New(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second) // slow server
	}))
	t.Cleanup(server.Close)

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // cancel immediately

	digest := sha256.Sum256([]byte("cancel test"))
	_, err := RequestTimestamp(ctx, server.Client(), server.URL, crypto.SHA256, digest[:])
	r.Error(err)
}

// --- Mock TSA server ---

func mustTSAKeyAndCert(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	r := require.New(t)
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	r.NoError(err)

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Test TSA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		ExtraExtensions: []pkix.Extension{{
			Id:       asn1.ObjectIdentifier{2, 5, 29, 37}, // id-ce-extKeyUsage
			Critical: true,
			Value: func() []byte {
				val, err := asn1.Marshal([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 8}})
				r.NoError(err)
				return val
			}(),
		}},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)

	cert, err := x509.ParseCertificate(der)
	r.NoError(err)
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

func TestRedactURL(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"plain", "https://tsa.example/ts", "https://tsa.example/ts"},
		{"userinfo", "https://user:token@tsa.example/ts", "https://tsa.example/ts"},
		{"query", "https://tsa.example/ts?key=secret", "https://tsa.example/ts"},
		{"fragment", "https://tsa.example/ts#frag", "https://tsa.example/ts"},
		{"opaque credentials", "user:pass@tsa.example/ts", "<invalid TSA URL>"},
		{"unparseable", "https://tsa.example/ts\x7f%zz", "<invalid TSA URL>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			got := RedactURL(tc.in)
			r.Equal(tc.want, got)
			// A redacted URL must never re-expose credential-bearing input.
			r.NotContains(got, "token")
			r.NotContains(got, "pass")
			r.NotContains(got, "secret")
		})
	}
}

func TestSanitizeURL(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"plain", "https://tsa.example/ts", "https://tsa.example/ts", false},
		{"port and path kept", "https://tsa.example:8443/api/ts", "https://tsa.example:8443/api/ts", false},
		{"userinfo stripped", "https://user:token@tsa.example/ts", "https://tsa.example/ts", false},
		{"query stripped", "https://tsa.example/ts?key=secret", "https://tsa.example/ts", false},
		{"fragment stripped", "https://tsa.example/ts#frag", "https://tsa.example/ts", false},
		{"http local kept", "http://127.0.0.1:1234/ts", "http://127.0.0.1:1234/ts", false},
		{"opaque rejected", "user:pass@tsa.example/ts", "", true},
		{"unparseable rejected", "https://tsa.example/ts\x7f%zz", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			got, err := SanitizeURL(tc.in)
			if tc.wantErr {
				r.Error(err)
				r.Empty(got)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
			r.NotContains(got, "token")
			r.NotContains(got, "secret")
		})
	}
}

func TestRejectInsecureRedirect(t *testing.T) {
	r := require.New(t)

	httpsReq, err := http.NewRequest(http.MethodPost, "https://tsa.example/ts", nil)
	r.NoError(err)
	r.NoError(RejectInsecureRedirect(httpsReq, nil))

	httpReq, err := http.NewRequest(http.MethodPost, "http://tsa.example/ts", nil)
	r.NoError(err)
	err = RejectInsecureRedirect(httpReq, nil)
	r.Error(err)
	r.Contains(err.Error(), "insecure redirect")

	// A custom CheckRedirect disables net/http's default 10-redirect cap, so the
	// policy must stop once the limit is reached even for HTTPS targets.
	via := make([]*http.Request, maxTSARedirects)
	err = RejectInsecureRedirect(httpsReq, via)
	r.Error(err)
	r.Contains(err.Error(), "stopped after")
}

// issueTimestampingCert issues a certificate signed by parent (self-signed when
// parent/parentKey are nil) carrying the critical id-kp-timeStamping EKU.
func issueTimestampingCert(t *testing.T, cn string, isCA bool, parent *x509.Certificate, parentKey *rsa.PrivateKey) (*rsa.PrivateKey, *x509.Certificate) {
	r := require.New(t)
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	r.NoError(err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	r.NoError(err)

	ekuVal, err := asn1.Marshal([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 8}})
	r.NoError(err)

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(7 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  isCA,
		ExtraExtensions: []pkix.Extension{{
			Id:       asn1.ObjectIdentifier{2, 5, 29, 37},
			Critical: true,
			Value:    ekuVal,
		}},
	}
	signerCert, signerKey := tmpl, key
	if parent != nil {
		signerCert, signerKey = parent, parentKey
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signerCert, &key.PublicKey, signerKey)
	r.NoError(err)
	cert, err := x509.ParseCertificate(der)
	r.NoError(err)
	return key, cert
}

// newChainMockTSAHandler serves timestamp tokens whose SignedData embeds the
// signer leaf together with the supplied intermediate certificates.
func newChainMockTSAHandler(t *testing.T, leaf *x509.Certificate, leafKey *rsa.PrivateKey, chain []*x509.Certificate) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// require's FailNow must only run on the test goroutine; this handler runs
		// on a server goroutine, so report with t.Errorf and abort the response.
		fail := func(status int, msg string, err error) {
			t.Errorf("%s: %v", msg, err)
			http.Error(w, msg, status)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			fail(http.StatusBadRequest, "read body", err)
			return
		}
		var req Request
		if _, err = asn1.Unmarshal(body, &req); err != nil {
			fail(http.StatusBadRequest, "unmarshal request", err)
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
			fail(http.StatusInternalServerError, "marshal tstinfo", err)
			return
		}

		sd, err := pkcs7.NewSignedData(tstInfoDER)
		if err != nil {
			fail(http.StatusInternalServerError, "new signed data", err)
			return
		}
		sd.SetContentType(asn1.ObjectIdentifier{1, 2, 840, 113549, 1, 9, 16, 1, 4})
		if err := sd.AddSigner(leaf, leafKey, pkcs7.SignerInfoConfig{}); err != nil {
			fail(http.StatusInternalServerError, "add signer", err)
			return
		}
		for _, c := range chain {
			sd.AddCertificate(c)
		}
		p7DER, err := sd.Finish()
		if err != nil {
			fail(http.StatusInternalServerError, "finish", err)
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
			fail(http.StatusInternalServerError, "marshal response", err)
			return
		}
		w.Header().Set("Content-Type", "application/timestamp-reply")
		_, _ = w.Write(respDER)
	})
}

// TestVerify_RootIntermediateLeafChain proves the token's embedded intermediate
// is used to complete a root→intermediate→TSA chain when only the root is
// trusted; without loading intermediates this would fail.
func TestVerify_RootIntermediateLeafChain(t *testing.T) {
	r := require.New(t)

	rootKey, rootCert := issueTimestampingCert(t, "Test Root", true, nil, nil)
	interKey, interCert := issueTimestampingCert(t, "Test Intermediate", true, rootCert, rootKey)
	leafKey, leafCert := issueTimestampingCert(t, "Test TSA Leaf", false, interCert, interKey)

	// The token embeds the intermediate (and leaf); only the root is trusted.
	server := httptest.NewServer(newChainMockTSAHandler(t, leafCert, leafKey, []*x509.Certificate{interCert}))
	t.Cleanup(server.Close)

	digest := sha256.Sum256([]byte("chain test"))
	token, err := RequestTimestamp(t.Context(), server.Client(), server.URL, crypto.SHA256, digest[:])
	r.NoError(err)

	roots := x509.NewCertPool()
	roots.AddCert(rootCert)
	_, trusted, err := Verify(token.Raw, crypto.SHA256, digest[:], roots)
	r.NoError(err)
	r.True(trusted)
}

func TestCredentialTypes(t *testing.T) {
	r := require.New(t)
	types := CredentialTypes{}
	r.True(types.GetCredentialTypeScheme().IsRegistered(tsacredentialsv1alpha1.VersionedType))
	r.True(types.GetConsumerIdentityTypeScheme().IsRegistered(runtime.NewVersionedType("TSA", "v1alpha1")))
	r.True(types.GetConsumerIdentityTypeScheme().IsRegistered(runtime.NewUnversionedType("TSA")))
}
