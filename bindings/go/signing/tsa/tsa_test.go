package tsa

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
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

	"github.com/digitorus/timestamp"
	"github.com/stretchr/testify/require"

	credconfigv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	tsacredentialsv1alpha1 "ocm.software/open-component-model/bindings/go/signing/tsa/spec/credentials/v1alpha1"
)

var genTime = time.Now().UTC().Truncate(time.Second)

// newCert issues a certificate signed by parent (self-signed when parent is nil)
// carrying the critical id-kp-timeStamping EKU. mutate may adjust the template.
func newCert(t *testing.T, cn string, parent *x509.Certificate, parentKey *rsa.PrivateKey, mutate func(*x509.Certificate)) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	r := require.New(t)
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
		IsCA:                  true,
		ExtraExtensions:       []pkix.Extension{{Id: oidExtKeyUsage, Critical: true, Value: ekuVal}},
	}
	if mutate != nil {
		mutate(tmpl)
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

// newResponse returns a granted DER TimeStampResp for ts, signed by cert/key.
func newResponse(cert *x509.Certificate, key *rsa.PrivateKey, ts timestamp.Timestamp) ([]byte, error) {
	ts.Policy = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0}
	return ts.CreateResponseWithOpts(cert, key, crypto.SHA256)
}

// newToken returns a DER timestamp token over digest, signed by cert/key and
// embedding the signer certificate. mutate may adjust the token contents. The
// token is extracted without verification, so it may carry an invalid signature.
func newToken(t *testing.T, cert *x509.Certificate, key *rsa.PrivateKey, digest []byte, mutate func(*timestamp.Timestamp)) []byte {
	t.Helper()
	r := require.New(t)
	ts := timestamp.Timestamp{HashAlgorithm: crypto.SHA256, HashedMessage: digest, Time: genTime, AddTSACertificate: true}
	if mutate != nil {
		mutate(&ts)
	}
	resp, err := newResponse(cert, key, ts)
	r.NoError(err)
	var parsed struct {
		Status asn1.RawValue
		Token  asn1.RawValue
	}
	_, err = asn1.Unmarshal(resp, &parsed)
	r.NoError(err)
	return parsed.Token.FullBytes
}

// newMockTSAHandler answers timestamp requests with tokens signed by cert/key.
// mutate may adjust the token derived from the request.
func newMockTSAHandler(t *testing.T, cert *x509.Certificate, key *rsa.PrivateKey, mutate func(*timestamp.Timestamp)) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// require's FailNow must only run on the test goroutine; this handler runs
		// on a server goroutine, so report with t.Errorf and abort the response.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request: %v", err)
			http.Error(w, "read body", http.StatusBadRequest)
			return
		}
		req, err := timestamp.ParseRequest(body)
		if err != nil {
			t.Errorf("parsing request: %v", err)
			http.Error(w, "parse request", http.StatusBadRequest)
			return
		}
		ts := timestamp.Timestamp{
			HashAlgorithm:     req.HashAlgorithm,
			HashedMessage:     req.HashedMessage,
			Nonce:             req.Nonce,
			Time:              genTime,
			AddTSACertificate: req.Certificates,
		}
		if mutate != nil {
			mutate(&ts)
		}
		resp, err := newResponse(cert, key, ts)
		if err != nil {
			t.Errorf("creating response: %v", err)
			http.Error(w, "create response", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/timestamp-reply")
		_, _ = w.Write(resp)
	})
}

func TestPEM(t *testing.T) {
	raw := []byte{0x30, 0x82, 0x01, 0x00, 0xDE, 0xAD, 0xBE, 0xEF}
	legacy := pem.EncodeToMemory(&pem.Block{Type: "TIMESTAMP INFO", Bytes: []byte{0x30, 0x00}})

	tests := []struct {
		name       string
		data       []byte
		decode     func([]byte) ([]byte, error)
		want       []byte
		wantLegacy bool
		wantErr    string
	}{
		{name: "token round trip", data: ToPEM(raw), decode: FromPEM, want: raw},
		{name: "no PEM block", data: []byte("not a pem block"), decode: FromPEM, wantErr: "no PEM block found"},
		{name: "wrong block type", data: []byte("-----BEGIN CERTIFICATE-----\nZm9v\n-----END CERTIFICATE-----\n"), decode: FromPEM, wantErr: "unexpected PEM block type"},
		{name: "trailing data", data: append(ToPEM([]byte{0xDE, 0xAD}), []byte("-----BEGIN EXTRA-----\nZm9v\n-----END EXTRA-----\n")...), decode: FromPEM, wantErr: "trailing data"},
		{name: "legacy block is not a token", data: legacy, decode: FromPEM, wantLegacy: true, wantErr: "unexpected PEM block type"},
		{name: "legacy decoder rejects non-PEM", data: []byte("garbage"), decode: FromLegacyPEM, wantErr: "no PEM block found"},
		{name: "legacy decoder rejects a token block", data: ToPEM([]byte{0x30, 0x00}), decode: FromLegacyPEM, wantErr: "unexpected PEM block type"},
		{name: "legacy trailing data", data: append(bytes.Clone(legacy), []byte("extra")...), decode: FromLegacyPEM, wantLegacy: true, wantErr: "trailing data"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			r.Equal(tc.wantLegacy, IsLegacyPEM(tc.data))
			got, err := tc.decode(tc.data)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}

func TestFromLegacyPEM_RoundTripVerifies(t *testing.T) {
	r := require.New(t)
	tsaKey, tsaCert := newCert(t, "Test TSA", nil, nil, nil)
	digest := sha256.Sum256([]byte("descriptor digest"))
	token := newToken(t, tsaCert, tsaKey, digest[:], nil)

	// The legacy OCM CLI stores the bare SignedData, without the ContentInfo wrapper.
	var contentInfo struct {
		ContentType asn1.ObjectIdentifier
		Content     asn1.RawValue `asn1:"explicit,tag:0"`
	}
	_, err := asn1.Unmarshal(token, &contentInfo)
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

func TestRootCertPoolFromCredentials(t *testing.T) {
	_, cert := newCert(t, "Test TSA", nil, nil, nil)
	pemData := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}))
	pemFile := filepath.Join(t.TempDir(), "roots.pem")
	require.NoError(t, os.WriteFile(pemFile, []byte(pemData), 0o600))

	tests := []struct {
		name     string
		creds    runtime.Typed
		wantPool bool
		wantErr  string
	}{
		{name: "typed inline PEM", creds: &tsacredentialsv1alpha1.TSACredentials{Type: tsacredentialsv1alpha1.VersionedType, RootCertsPEM: pemData}, wantPool: true},
		{name: "typed PEM file", creds: &tsacredentialsv1alpha1.TSACredentials{Type: tsacredentialsv1alpha1.VersionedType, RootCertsPEMFile: pemFile}, wantPool: true},
		{name: "missing PEM file", creds: &tsacredentialsv1alpha1.TSACredentials{Type: tsacredentialsv1alpha1.VersionedType, RootCertsPEMFile: filepath.Join(t.TempDir(), "missing.pem")}, wantErr: "reading root certificates"},
		{
			name: "direct credentials",
			creds: &credconfigv1.DirectCredentials{
				Type:       runtime.NewVersionedType(credconfigv1.CredentialsType, credconfigv1.Version),
				Properties: map[string]string{"rootCertsPEM": pemData},
			},
			wantPool: true,
		},
		{name: "invalid PEM", creds: &tsacredentialsv1alpha1.TSACredentials{Type: tsacredentialsv1alpha1.VersionedType, RootCertsPEM: "not valid PEM data"}, wantErr: "no valid certificates"},
		{name: "no credentials"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			pool, err := RootCertPoolFromCredentials(tc.creds)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantPool, pool != nil)
		})
	}
}

func TestRequestTimestamp(t *testing.T) {
	tsaKey, tsaCert := newCert(t, "Test TSA", nil, nil, nil)
	roots := x509.NewCertPool()
	roots.AddCert(tsaCert)
	digest := sha256.Sum256([]byte("test"))

	writeDER := func(der []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/timestamp-reply")
			_, _ = w.Write(der)
		}
	}
	rejection, err := timestamp.CreateErrorResponse(timestamp.Rejection, timestamp.BadRequest)
	require.NoError(t, err)
	notPKCS7, err := asn1.Marshal(struct {
		Status         struct{ Status int }
		TimeStampToken asn1.RawValue
	}{TimeStampToken: asn1.RawValue{FullBytes: []byte{0x30, 0x03, 0x01, 0x01, 0xFF}}})
	require.NoError(t, err)
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()

	tests := []struct {
		name string
		ctx  context.Context
		// handler, when set, is served by a test server whose URL replaces url.
		handler   http.Handler
		url       string
		hash      crypto.Hash
		digest    []byte
		wantErr   string
		wantErrIs error
	}{
		{name: "granted", handler: newMockTSAHandler(t, tsaCert, tsaKey, nil)},
		{name: "unsupported hash", url: "http://example.com", hash: crypto.MD5, digest: []byte("x"), wantErr: "unsupported hash"},
		{name: "digest length mismatch", url: "http://example.com", hash: crypto.SHA512, wantErr: "does not match"},
		{name: "invalid URL", url: "://bad-url", wantErr: "parsing TSA URL"},
		// Port 1 on loopback is closed, so the dial fails without external traffic.
		{name: "transport error redacts URL", url: "http://alice@127.0.0.1:1/tsa?apikey=S3CR3T", wantErr: "http://127.0.0.1:1/tsa"},
		{name: "context cancelled", ctx: cancelled, url: "http://127.0.0.1:1/tsa", wantErrIs: context.Canceled},
		{
			name: "server error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "internal error", http.StatusInternalServerError)
			}),
			wantErr: "HTTP 500",
		},
		{name: "malformed response", handler: writeDER([]byte("not valid asn1")), wantErr: "parsing timestamp response"},
		{name: "rejection status", handler: writeDER(rejection), wantErr: "request is rejected"},
		{name: "trailing data", handler: writeDER(append(bytes.Clone(rejection), 0x00, 0x00)), wantErr: "trailing data"},
		{name: "token is not PKCS#7", handler: writeDER(notPKCS7), wantErr: "parsing timestamp response"},
		{
			name: "mismatched imprint",
			handler: newMockTSAHandler(t, tsaCert, tsaKey, func(ts *timestamp.Timestamp) {
				ts.HashedMessage = bytes.Clone(ts.HashedMessage)
				ts.HashedMessage[0] ^= 0xFF
			}),
			wantErr: "message imprint does not match",
		},
		{
			name:    "mismatched imprint hash algorithm",
			handler: newMockTSAHandler(t, tsaCert, tsaKey, func(ts *timestamp.Timestamp) { ts.HashAlgorithm = crypto.SHA384 }),
			wantErr: "message imprint does not match",
		},
		{
			name:    "mismatched nonce",
			handler: newMockTSAHandler(t, tsaCert, tsaKey, func(ts *timestamp.Timestamp) { ts.Nonce = big.NewInt(999999) }),
			wantErr: "nonce does not match",
		},
		{
			name:    "missing nonce",
			handler: newMockTSAHandler(t, tsaCert, tsaKey, func(ts *timestamp.Timestamp) { ts.Nonce = nil }),
			wantErr: "nonce does not match",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			url, ctx, hash, imprint := tc.url, t.Context(), crypto.SHA256, digest[:]
			if tc.handler != nil {
				server := httptest.NewServer(tc.handler)
				t.Cleanup(server.Close)
				url = server.URL
			}
			if tc.ctx != nil {
				ctx = tc.ctx
			}
			if tc.hash != 0 {
				hash = tc.hash
			}
			if tc.digest != nil {
				imprint = tc.digest
			}

			// A nil client falls back to http.DefaultClient.
			token, err := RequestTimestamp(ctx, nil, url, hash, imprint)
			switch {
			case tc.wantErrIs != nil:
				r.ErrorIs(err, tc.wantErrIs)
			case tc.wantErr != "":
				r.ErrorContains(err, tc.wantErr)
				r.NotContains(err.Error(), "S3CR3T")
				r.NotContains(err.Error(), "alice")
			default:
				r.NoError(err)
				verifiedTime, trusted, err := Verify(token.Raw, hash, imprint, roots)
				r.NoError(err)
				r.True(trusted)
				r.Equal(genTime, token.Time)
				r.Equal(token.Time, verifiedTime)
			}
		})
	}
}

func TestVerify(t *testing.T) {
	digest := sha256.Sum256([]byte("original"))
	wrongDigest := sha256.Sum256([]byte("tampered"))

	tsaKey, tsaCert := newCert(t, "Test TSA", nil, nil, nil)
	roots := x509.NewCertPool()
	roots.AddCert(tsaCert)
	token := newToken(t, tsaCert, tsaKey, digest[:], nil)

	_, otherCert := newCert(t, "Other TSA", nil, nil, nil)
	otherRoots := x509.NewCertPool()
	otherRoots.AddCert(otherCert)

	rootKey, rootCert := newCert(t, "Test Root", nil, nil, nil)
	interKey, interCert := newCert(t, "Test Intermediate", rootCert, rootKey, nil)
	leafKey, leafCert := newCert(t, "Test TSA Leaf", interCert, interKey, func(c *x509.Certificate) { c.IsCA = false })
	chainRoots := x509.NewCertPool()
	chainRoots.AddCert(rootCert)

	serverAuthEKU, err := asn1.Marshal([]asn1.ObjectIdentifier{{1, 3, 6, 1, 5, 5, 7, 3, 1}})
	require.NoError(t, err)
	serverAuthKey, serverAuthCert := newCert(t, "Non-TSA", nil, nil, func(c *x509.Certificate) {
		c.ExtraExtensions = []pkix.Extension{{Id: oidExtKeyUsage, Critical: true, Value: serverAuthEKU}}
	})
	// x509.CreateCertificate emits the ExtKeyUsage field as a non-critical extension.
	nonCriticalKey, nonCriticalCert := newCert(t, "Non-critical EKU", nil, nil, func(c *x509.Certificate) {
		c.ExtraExtensions = nil
		c.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}
	})
	_, mismatchedCert := newCert(t, "Mismatched key", nil, nil, nil)

	tests := []struct {
		name        string
		raw         []byte
		hash        crypto.Hash
		digest      []byte
		roots       *x509.CertPool
		wantTrusted bool
		wantErr     string
	}{
		{name: "trusted with configured root", raw: token, hash: crypto.SHA256, digest: digest[:], roots: roots, wantTrusted: true},
		{name: "structural only without roots", raw: token, hash: crypto.SHA256, digest: digest[:]},
		{
			name: "root, intermediate and leaf chain with only the root trusted",
			raw: newToken(t, leafCert, leafKey, digest[:], func(ts *timestamp.Timestamp) {
				ts.Certificates = []*x509.Certificate{interCert}
			}),
			hash: crypto.SHA256, digest: digest[:], roots: chainRoots, wantTrusted: true,
		},
		{name: "not DER", raw: []byte("not DER"), hash: crypto.SHA256, digest: digest[:], wantErr: "parsing timestamp token"},
		{name: "unsupported hash", raw: token, hash: crypto.MD5, digest: []byte("x"), wantErr: "unsupported hash"},
		{name: "wrong digest", raw: token, hash: crypto.SHA256, digest: wrongDigest[:], roots: roots, wantErr: "does not match"},
		{
			name: "same imprint bytes under another hash algorithm",
			raw:  newToken(t, tsaCert, tsaKey, digest[:], func(ts *timestamp.Timestamp) { ts.HashAlgorithm = crypto.SHA384 }),
			hash: crypto.SHA256, digest: digest[:], roots: roots, wantErr: "does not match",
		},
		{name: "untrusted root", raw: token, hash: crypto.SHA256, digest: digest[:], roots: otherRoots, wantErr: "verifying PKCS#7 signer certificate chain"},
		{
			name: "invalid CMS signature",
			raw:  newToken(t, mismatchedCert, tsaKey, digest[:], nil),
			hash: crypto.SHA256, digest: digest[:], wantErr: "parsing timestamp token",
		},
		{
			name: "no embedded signer certificate",
			raw:  newToken(t, tsaCert, tsaKey, digest[:], func(ts *timestamp.Timestamp) { ts.AddTSACertificate = false }),
			hash: crypto.SHA256, digest: digest[:], roots: roots, wantErr: "exactly one signer",
		},
		{
			name: "non-timestamping EKU",
			raw:  newToken(t, serverAuthCert, serverAuthKey, digest[:], nil),
			hash: crypto.SHA256, digest: digest[:], wantErr: "id-kp-timeStamping as its only extended key usage",
		},
		{
			name: "non-critical EKU",
			raw:  newToken(t, nonCriticalCert, nonCriticalKey, digest[:], nil),
			hash: crypto.SHA256, digest: digest[:], wantErr: "not marked critical",
		},
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
			r.Equal(genTime, verifiedTime)
		})
	}
}

func TestSanitizeURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{name: "plain", in: "https://tsa.example/ts", want: "https://tsa.example/ts"},
		{name: "port and path kept", in: "https://tsa.example:8443/api/ts", want: "https://tsa.example:8443/api/ts"},
		{name: "userinfo stripped", in: "https://user:token@tsa.example/ts", want: "https://tsa.example/ts"},
		{name: "query stripped", in: "https://tsa.example/ts?key=secret", want: "https://tsa.example/ts"},
		{name: "fragment stripped", in: "https://tsa.example/ts#frag", want: "https://tsa.example/ts"},
		{name: "http local kept", in: "http://127.0.0.1:1234/ts", want: "http://127.0.0.1:1234/ts"},
		{name: "opaque rejected", in: "user:pass@tsa.example/ts", wantErr: true},
		{name: "unparseable rejected", in: "https://tsa.example/ts\x7f%zz", wantErr: true},
	}
	for _, tc := range tests {
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
		})
	}
}
