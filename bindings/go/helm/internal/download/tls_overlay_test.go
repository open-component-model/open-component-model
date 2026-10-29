package download

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

// TestNewReadOnlyChartFromRemote_InputTLSOverridesHTTPConfig pins that with an HTTP configuration
// both the OCI pull and the HTTP/S download use its client, and that a CA of the input replaces
// the roots of that configuration.
func TestNewReadOnlyChartFromRemote_InputTLSOverridesHTTPConfig(t *testing.T) {
	tgz, err := os.ReadFile("../../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)

	reg, manifest := newChartRegistry(t, "charts/mychart", "0.1.0", tgz, nil)
	ociRef := "oci://" + strings.TrimPrefix(reg.URL, "https://") + "/charts/mychart:0.1.0"

	files := httptest.NewTLSServer(http.FileServer(http.Dir("../../testdata")))
	t.Cleanup(files.Close)
	httpRef := files.URL + "/mychart-0.1.0.tgz"

	// httptest servers share one certificate, so the wrong CA is generated.
	other := foreignCAPEM(t)
	right := certPEM(reg)

	config := func(rootsPEM string) *httpv1alpha1.Config {
		return &httpv1alpha1.Config{TLSConfig: httpv1alpha1.TLSConfig{RootCAsPEM: rootsPEM}}
	}

	tests := []struct {
		name    string
		opts    []Option
		wantErr string
	}{
		{name: "HTTP configuration roots", opts: []Option{WithHTTPConfig(config(right))}},
		{name: "input CA overrides wrong HTTP configuration roots", opts: []Option{WithHTTPConfig(config(other)), WithCACert(right)}},
		{name: "wrong input CA overrides HTTP configuration roots", opts: []Option{WithHTTPConfig(config(right)), WithCACert(other)}, wantErr: "certificate"},
	}
	for _, ref := range []struct{ name, ref string }{{"oci tag", ociRef}, {"oci pinned", ociRef}, {"https", httpRef}} {
		for _, tc := range tests {
			t.Run(ref.name+"/"+tc.name, func(t *testing.T) {
				r := require.New(t)
				opts := tc.opts
				if ref.name == "oci pinned" {
					opts = append(opts[:len(opts):len(opts)], WithOCIManifestDigest(manifest))
				}
				result, err := NewReadOnlyChartFromRemote(t.Context(), ref.ref, t.TempDir(), opts...)
				if tc.wantErr != "" {
					r.ErrorContains(err, tc.wantErr)
					return
				}
				r.NoError(err)
				r.Equal("mychart", result.Name)
				r.Equal("0.1.0", result.Version)
			})
		}
	}
}

// TestNewReadOnlyChartFromRemote_HTTPConfigKeepsOCICredentials pins that the registry client built
// from an HTTP configuration authenticates the OCI chart pull with the resolved credentials.
func TestNewReadOnlyChartFromRemote_HTTPConfigKeepsOCICredentials(t *testing.T) {
	tgz, err := os.ReadFile("../../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)

	reg, _ := newChartRegistry(t, "charts/mychart", "0.1.0", tgz, nil)
	target, err := url.Parse(reg.URL)
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = reg.Client().Transport
	private := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if user, pass, ok := req.BasicAuth(); !ok || user != "user" || pass != "secret" {
			w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		proxy.ServeHTTP(w, req)
	}))
	t.Cleanup(private.Close)
	ociRef := "oci://" + strings.TrimPrefix(private.URL, "https://") + "/charts/mychart:0.1.0"
	httpConfig := WithHTTPConfig(&httpv1alpha1.Config{TLSConfig: httpv1alpha1.TLSConfig{RootCAsPEM: certPEM(private)}})

	tests := []struct {
		name    string
		creds   *ocicredsv1.OCICredentials
		wantErr string
	}{
		{name: "resolved credentials", creds: &ocicredsv1.OCICredentials{Username: "user", Password: "secret"}},
		{name: "wrong credentials", creds: &ocicredsv1.OCICredentials{Username: "user", Password: "wrong"}, wantErr: "401"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			result, err := NewReadOnlyChartFromRemote(t.Context(), ociRef, t.TempDir(), httpConfig, WithOCICredentials(tc.creds))
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal("mychart", result.Name)
		})
	}
}

func certPEM(srv *httptest.Server) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
}

func foreignCAPEM(t *testing.T) string {
	t.Helper()
	r := require.New(t)
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	r.NoError(err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "foreign CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	r.NoError(err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}
