package httpauth_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	"ocm.software/open-component-model/bindings/go/wget/httpauth"
	credv1 "ocm.software/open-component-model/bindings/go/wget/spec/credentials/v1"
)

// TestApply_MTLSKeepsClientConfiguration asserts that a client certificate is
// installed on the client's own transport chain instead of replacing it, so
// settings such as the user agent and disabled compression still apply, and
// that the single-use clone does not leave idle connections behind.
func TestApply_MTLSKeepsClientConfiguration(t *testing.T) {
	r := require.New(t)

	var peerCerts int
	var userAgent, acceptEncoding string
	var connClose bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		peerCerts = len(req.TLS.PeerCertificates)
		userAgent, acceptEncoding = req.UserAgent(), req.Header.Get("Accept-Encoding")
		connClose = req.Close
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	creds := mtlsCredentials(t, srv.Certificate())
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	r.NoError(err)
	client := ocmhttp.New(ocmhttp.WithUserAgent("ocm-test"))
	r.NoError(httpauth.Apply(t.Context(), req, &client, creds))

	resp, err := client.Do(req)
	r.NoError(err)
	r.NoError(resp.Body.Close())
	r.Equal(http.StatusOK, resp.StatusCode)
	r.Equal(1, peerCerts, "the client certificate must be presented")
	r.Equal("ocm-test", userAgent, "the client's user agent transport must be kept")
	r.Empty(acceptEncoding, "the client's disabled compression must be kept")
	r.True(connClose, "the per-request clone must not keep the connection alive")
}

// TestApply_MTLSOverridesClientCertificateSelector asserts that the wget
// credential is presented even if the client already selects its certificate
// via GetClientCertificate, which Go would otherwise prefer over Certificates.
func TestApply_MTLSOverridesClientCertificateSelector(t *testing.T) {
	r := require.New(t)

	var peerCerts int
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		peerCerts = len(req.TLS.PeerCertificates)
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			return &tls.Certificate{}, nil
		},
	}}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, nil)
	r.NoError(err)
	r.NoError(httpauth.Apply(t.Context(), req, &client, mtlsCredentials(t, srv.Certificate())))

	resp, err := client.Do(req)
	r.NoError(err)
	r.NoError(resp.Body.Close())
	r.Equal(http.StatusOK, resp.StatusCode)
	r.Equal(1, peerCerts, "the wget client certificate must be presented")
}

// mtlsCredentials returns wget credentials with a fresh self-signed client
// certificate that trusts serverCert as certificate authority.
func mtlsCredentials(t *testing.T, serverCert *x509.Certificate) *credv1.WgetCredentials {
	t.Helper()
	r := require.New(t)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	r.NoError(err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)
	keyDER, err := x509.MarshalECPrivateKey(key)
	r.NoError(err)

	return &credv1.WgetCredentials{
		Type:                 credv1.WgetCredentialsVersionedType,
		Certificate:          string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		PrivateKey:           string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})),
		CertificateAuthority: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverCert.Raw})),
	}
}

func TestApply_Redirect(t *testing.T) {
	creds := &credv1.WgetCredentials{
		Type:     credv1.WgetCredentialsVersionedType,
		Username: "user",
		Password: "secret",
	}

	// final records the Authorization header of the redirected request; both servers listen
	// on 127.0.0.1, so net/http itself treats the redirect as same-host.
	var gotAuth string
	final := func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}
	plain := httptest.NewServer(http.HandlerFunc(final))
	t.Cleanup(plain.Close)
	secure := httptest.NewTLSServer(http.HandlerFunc(final))
	t.Cleanup(secure.Close)

	tests := []struct {
		name     string
		target   string
		wantAuth bool
	}{
		{"https to http drops credentials", plain.URL, false},
		{"https to https keeps credentials", secure.URL, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			gotAuth = ""
			origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				http.Redirect(w, req, tc.target+"/final", http.StatusFound)
			}))
			t.Cleanup(origin.Close)

			req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, origin.URL, nil)
			r.NoError(err)
			client := origin.Client()
			// origin and secure use different self-signed certificates; trust both.
			client.Transport.(*http.Transport).TLSClientConfig.RootCAs.AddCert(secure.Certificate())
			r.NoError(httpauth.Apply(t.Context(), req, &client, creds))

			resp, err := client.Do(req)
			r.NoError(err)
			r.NoError(resp.Body.Close())
			r.Equal(http.StatusOK, resp.StatusCode)
			if tc.wantAuth {
				r.NotEmpty(gotAuth)
			} else {
				r.Empty(gotAuth)
			}
		})
	}
}
