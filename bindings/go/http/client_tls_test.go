package http_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	nethttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

func selfSignedClientCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// TestCloneWithTLS asserts that a client certificate installed via CloneWithTLS
// reaches the server through every chain New builds, while the rest of the
// client's behaviour (TLS trust, user agent, disabled compression) is kept and
// the original client stays untouched.
func TestCloneWithTLS(t *testing.T) {
	t.Parallel()

	type seen struct {
		peerCerts                 int
		userAgent, acceptEncoding string
	}
	insecure := true
	noRetry := -1
	tlsCfg := httpv1alpha1.TLSConfig{InsecureSkipVerify: &insecure}
	tests := []struct {
		name string
		cfg  func(host string) *httpv1alpha1.Config
	}{
		{name: "global chain", cfg: func(string) *httpv1alpha1.Config {
			return &httpv1alpha1.Config{TLSConfig: tlsCfg, Retry: &httpv1alpha1.RetryConfig{MaxRetries: &noRetry}}
		}},
		{name: "per-host chain", cfg: func(host string) *httpv1alpha1.Config {
			return &httpv1alpha1.Config{
				Retry: &httpv1alpha1.RetryConfig{MaxRetries: &noRetry},
				Hosts: map[string]*httpv1alpha1.HostConfig{host: {TLSConfig: tlsCfg}},
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			got := make(chan seen, 1)
			srv := httptest.NewUnstartedServer(nethttp.HandlerFunc(func(_ nethttp.ResponseWriter, req *nethttp.Request) {
				got <- seen{len(req.TLS.PeerCertificates), req.UserAgent(), req.Header.Get("Accept-Encoding")}
			}))
			srv.TLS = &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAnyClientCert}
			srv.StartTLS()
			t.Cleanup(srv.Close)

			cert := selfSignedClientCert(t)
			client := ocmhttp.New(ocmhttp.WithConfig(tt.cfg(mustHost(t, srv.URL))), ocmhttp.WithUserAgent("ocm-test"))

			withCert, err := ocmhttp.CloneWithTLS(client, func(c *tls.Config) {
				c.Certificates = []tls.Certificate{cert}
			})
			r.NoError(err)

			resp, err := getURL(t, withCert, srv.URL)
			r.NoError(err)
			r.NoError(resp.Body.Close())
			r.Equal(nethttp.StatusOK, resp.StatusCode)
			s := <-got
			r.Equal(1, s.peerCerts, "the client certificate must be presented")
			r.Equal("ocm-test", s.userAgent, "the user agent transport must be kept")
			r.Empty(s.acceptEncoding, "compression must stay disabled")

			_, err = getURL(t, client, srv.URL)
			r.Error(err, "the original client must not present the certificate")
		})
	}
}

func TestCloneWithTLS_RejectsForeignTransport(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	foreign := &nethttp.Client{Transport: roundTripperFunc(func(*nethttp.Request) (*nethttp.Response, error) {
		return nil, nil
	})}
	_, err := ocmhttp.CloneWithTLS(foreign, func(*tls.Config) {})
	r.ErrorContains(err, "cannot configure TLS")
}

type roundTripperFunc func(*nethttp.Request) (*nethttp.Response, error)

func (f roundTripperFunc) RoundTrip(r *nethttp.Request) (*nethttp.Response, error) { return f(r) }
