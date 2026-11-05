package download

import (
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

// challengingRegistry serves a TLS registry that answers every request with a basic-auth
// challenge — which is what makes an oras client resolve and send credentials — and records
// the Authorization header each request carried. It returns those records, the registry's
// host:port, and a client trusting its certificate.
func challengingRegistry(t *testing.T) (*[]string, string, *http.Client) {
	t.Helper()
	seen := &[]string{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		*seen = append(*seen, req.Header.Get("Authorization"))
		w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	client := ocmhttp.New(ocmhttp.WithConfig(&httpv1alpha1.Config{
		TLSConfig: httpv1alpha1.TLSConfig{RootCAsPEM: caPEM},
	}))
	return seen, strings.TrimPrefix(srv.URL, "https://"), client
}

func basicAuthHeader(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}

func TestRegistryHost(t *testing.T) {
	tests := []struct {
		name string
		ref  string
		want string
	}{
		{name: "https url", ref: "https://repo.example.com/charts", want: "repo.example.com"},
		{name: "https url with port", ref: "https://repo.example.com:8443/charts", want: "repo.example.com:8443"},
		{name: "oci reference", ref: "oci://registry.example.com/charts/mychart:1.0.0", want: "registry.example.com"},
		{name: "bare oci reference", ref: "registry.example.com/charts/mychart:1.0.0", want: "registry.example.com"},
		{name: "bare oci reference with port", ref: "registry.example.com:5000/charts/mychart:1.0.0", want: "registry.example.com:5000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			got, err := registryHost(tc.ref)
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}
