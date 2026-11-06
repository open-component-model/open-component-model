// Package tsatest starts a mock RFC 3161 timestamp authority (TSA) for tests.
//
// [Start] serves timestamp requests over plain HTTP until the test ends. Its signer is a
// self-signed certificate with the critical id-kp-timeStamping extended key usage, so a test
// can trust it as the TSA root:
//
//	tsa := tsatest.Start(t)
//	rootPath := pkitest.WriteCerts(t, t.TempDir(), "tsa-root.pem", tsa.Cert)
//
// By default each token carries the time of its request. [WithTime] fixes that time, for
// example to backdate a signature, and [TSA.Requests] counts the requests served so far:
//
//	tsa := tsatest.Start(t, tsatest.WithTime(time.Now().Add(-48*time.Hour)))
//	before := tsa.Requests()
package tsatest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitorus/timestamp"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/pkitest"
)

var (
	oidExtKeyUsage    = asn1.ObjectIdentifier{2, 5, 29, 37}
	oidKPTimeStamping = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 3, 8}
	oidPolicy         = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 0}
)

// TSA is a running mock timestamp authority. Cert is its signing certificate and also the
// root a verifier must trust.
type TSA struct {
	URL  string
	Cert *x509.Certificate

	genTime  time.Time
	requests atomic.Int64
}

// Option configures [Start].
type Option func(*TSA)

// WithTime makes every token attest genTime instead of the time of its request. The
// certificate is then valid from an hour before genTime, so a backdated time still verifies.
func WithTime(genTime time.Time) Option {
	return func(tsa *TSA) {
		tsa.genTime = genTime
	}
}

// Requests returns how many requests the TSA received, valid or not.
func (tsa *TSA) Requests() int64 {
	return tsa.requests.Load()
}

// Start starts a mock TSA and stops it when the test ends. A token embeds the TSA
// certificate only when the request asks for it.
func Start(t *testing.T, opts ...Option) *TSA {
	t.Helper()
	tsa := &TSA{}
	for _, opt := range opts {
		opt(tsa)
	}

	key := pkitest.RSAKey(t)
	tsa.Cert = signer(t, key, tsa.genTime)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		tsa.requests.Add(1)
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		tsReq, err := timestamp.ParseRequest(body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		genTime := tsa.genTime
		if genTime.IsZero() {
			genTime = time.Now()
		}
		out, err := (&timestamp.Timestamp{
			HashAlgorithm:     tsReq.HashAlgorithm,
			HashedMessage:     tsReq.HashedMessage,
			Time:              genTime.UTC().Truncate(time.Second),
			Nonce:             tsReq.Nonce,
			Policy:            oidPolicy,
			AddTSACertificate: tsReq.Certificates,
		}).CreateResponseWithOpts(tsa.Cert, key, crypto.SHA256)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/timestamp-reply")
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	tsa.URL = srv.URL
	return tsa
}

// signer returns a self-signed TSA certificate for key. It is valid from an hour before now,
// or before genTime when that is earlier, until an hour after now.
func signer(t *testing.T, key *rsa.PrivateKey, genTime time.Time) *x509.Certificate {
	t.Helper()
	r := require.New(t)
	now := time.Now()
	notBefore := now
	if !genTime.IsZero() && genTime.Before(now) {
		notBefore = genTime
	}
	eku, err := asn1.Marshal([]asn1.ObjectIdentifier{oidKPTimeStamping})
	r.NoError(err)
	tmpl := &x509.Certificate{
		SerialNumber:          pkitest.SerialNumber(t),
		Subject:               pkix.Name{CommonName: "mock TSA"},
		NotBefore:             notBefore.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		ExtraExtensions:       []pkix.Extension{{Id: oidExtKeyUsage, Critical: true, Value: eku}},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)
	cert, err := x509.ParseCertificate(der)
	r.NoError(err)
	return cert
}
