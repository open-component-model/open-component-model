package download

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2/registry/remote/auth"

	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
)

const (
	testUser     = "victim"
	testPassword = "VICTIM-SHARED-REPOSITORY-PASSWORD"
)

func TestScopedAuthorizer(t *testing.T) {
	tests := []struct {
		name      string
		helmRepo  string
		target    string
		wantCreds bool
	}{
		{name: "http repository host", helmRepo: "https://repo.example.com/charts/mychart:1.0.0", target: "repo.example.com", wantCreds: true},
		{name: "oci repository host", helmRepo: "oci://registry.example.com/charts/mychart:1.0.0", target: "registry.example.com", wantCreds: true},
		{name: "repository host with a port", helmRepo: "https://repo.example.com:8443/charts", target: "repo.example.com:8443", wantCreds: true},
		{name: "bare oci reference", helmRepo: "registry.example.com/charts/mychart:1.0.0", target: "registry.example.com", wantCreds: true},
		{name: "chart resolved to another registry", helmRepo: "https://repo.example.com/charts/mychart:1.0.0", target: "attacker.invalid"},
		{name: "same host on another port", helmRepo: "https://repo.example.com/charts", target: "repo.example.com:1234"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			authorizer, err := scopedAuthorizer(tc.helmRepo, nil, testUser, testPassword)
			r.NoError(err)

			cred, err := authorizer.Credential(t.Context(), tc.target)
			r.NoError(err)
			if tc.wantCreds {
				r.Equal(auth.Credential{Username: testUser, Password: testPassword}, cred)
				return
			}
			r.Equal(auth.EmptyCredential, cred)
		})
	}
}

// TestNewClients_RegistryCredentialsStayOnTheRepositoryHost covers the credentials the
// registry client carries. Credentials are resolved for the Helm repository, but the chart
// URL an index.yaml resolves to need not live there: a remote or virtual repository names
// the upstream registry, so an index entry of "oci://<other host>/..." decides where the
// registry client sends them.
func TestNewClients_RegistryCredentialsStayOnTheRepositoryHost(t *testing.T) {
	r := require.New(t)

	var authSeen []string
	registrySrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		authSeen = append(authSeen, req.Header.Get("Authorization"))
		// The challenge is what makes an oras client resolve and send its
		// credentials for this host.
		w.Header().Set("WWW-Authenticate", `Basic realm="registry"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer registrySrv.Close()

	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: registrySrv.Certificate().Raw}))
	opt := &option{
		Credentials: &helmcredsv1.HelmHTTPCredentials{Username: testUser, Password: testPassword},
		HTTPConfig:  &httpv1alpha1.Config{TLSConfig: httpv1alpha1.TLSConfig{RootCAsPEM: caPEM}},
	}

	// The credentials belong to this repository, which is never dialled; the
	// chart they resolve to is served by registrySrv instead.
	const helmRepo = "https://repo.invalid/charts/mychart:0.1.0"
	_, _, regClient, err := newClients(opt, helmRepo, "", testUser, testPassword)
	r.NoError(err)

	resolved := strings.TrimPrefix(registrySrv.URL, "https://") + "/charts/mychart:0.1.0"
	_, err = regClient.Resolve(resolved)
	r.Error(err, "the registry answers 401 to every request")

	r.NotEmpty(authSeen, "the registry client must have reached the resolved host")
	for _, header := range authSeen {
		r.Empty(header, "the Helm repository credentials must not be sent to the resolved chart host")
	}
}
