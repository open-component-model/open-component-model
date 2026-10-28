package download

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	godigest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"

	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// TestNewReadOnlyChartFromRemote_InputTLSOverridesHTTPConfig pins that with an HTTP configuration
// both the OCI pull and the HTTP/S download use its client, and that a CA of the input replaces
// the roots of that configuration.
func TestNewReadOnlyChartFromRemote_InputTLSOverridesHTTPConfig(t *testing.T) {
	tgz, err := os.ReadFile("../../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)

	reg := newChartRegistry(t, "charts/mychart", "0.1.0", tgz)
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
	for _, ref := range []struct{ name, ref string }{{"oci", ociRef}, {"https", httpRef}} {
		for _, tc := range tests {
			t.Run(ref.name+"/"+tc.name, func(t *testing.T) {
				r := require.New(t)
				result, err := NewReadOnlyChartFromRemote(t.Context(), ref.ref, t.TempDir(), tc.opts...)
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

// newChartRegistry serves tgz as chart repo:tag from a TLS OCI registry.
func newChartRegistry(t *testing.T, repo, tag string, tgz []byte) *httptest.Server {
	t.Helper()
	config := []byte(`{"apiVersion":"v2","name":"mychart","version":"0.1.0"}`)
	manifest, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config: ociImageSpecV1.Descriptor{
			MediaType: registry.ConfigMediaType,
			Digest:    godigest.FromBytes(config),
			Size:      int64(len(config)),
		},
		Layers: []ociImageSpecV1.Descriptor{{
			MediaType: registry.ChartLayerMediaType,
			Digest:    godigest.FromBytes(tgz),
			Size:      int64(len(tgz)),
		}},
	})
	require.NoError(t, err)

	manifests := map[string][]byte{tag: manifest, godigest.FromBytes(manifest).String(): manifest}
	blobs := map[string][]byte{
		godigest.FromBytes(config).String(): config,
		godigest.FromBytes(tgz).String():    tgz,
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		rest, ok := strings.CutPrefix(req.URL.Path, "/v2/"+repo+"/")
		if !ok {
			http.NotFound(w, req)
			return
		}
		var data []byte
		mediaType := "application/octet-stream"
		if ref, isManifest := strings.CutPrefix(rest, "manifests/"); isManifest {
			data, mediaType = manifests[ref], ociImageSpecV1.MediaTypeImageManifest
		} else if ref, isBlob := strings.CutPrefix(rest, "blobs/"); isBlob {
			data = blobs[ref]
		}
		if data == nil {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Docker-Content-Digest", godigest.FromBytes(data).String())
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if req.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
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
