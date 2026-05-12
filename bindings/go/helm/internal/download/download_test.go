package download

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
)

func TestGetVersion(t *testing.T) {
	tests := []struct {
		name            string
		versionOverride string
		helmRepo        string
		expectedVersion string
		expectError     bool
		errorContains   string
	}{
		{
			name:            "OCI repo with tag and no override",
			versionOverride: "",
			helmRepo:        "oci://registry.example.com/charts/mychart:1.2.3",
			expectedVersion: "1.2.3",
		},
		{
			name:            "OCI repo without tag and no override returns error",
			versionOverride: "",
			helmRepo:        "oci://registry.example.com/charts/mychart",
			expectError:     true,
			errorContains:   "either helm repository tag or spec.Version has to be set",
		},
		{
			name:            "OCI repo with version override",
			versionOverride: "2.0.0",
			helmRepo:        "oci://registry.example.com/charts/mychart:1.2.3",
			expectedVersion: "2.0.0",
		},
		{
			name:            "OCI repo without tag but with version override",
			versionOverride: "3.0.0",
			helmRepo:        "oci://registry.example.com/charts/mychart",
			expectedVersion: "3.0.0",
		},
		{
			name:            "HTTPS repo with no override returns empty version",
			versionOverride: "",
			helmRepo:        "https://example.com/charts/mychart-1.0.0.tgz",
			expectedVersion: "",
		},
		{
			name:            "HTTPS repo with version override",
			versionOverride: "1.0.0",
			helmRepo:        "https://example.com/charts/mychart-1.0.0.tgz",
			expectedVersion: "1.0.0",
		},
		{
			name:            "HTTPS repo with version different override",
			versionOverride: "1.2.0",
			helmRepo:        "https://example.com/charts/mychart-1.0.0.tgz",
			expectedVersion: "1.2.0",
		},
		{
			name:            "HTTP repo with no override returns empty version",
			versionOverride: "",
			helmRepo:        "http://example.com/charts/mychart-1.0.0.tgz",
			expectedVersion: "",
		},
		{
			name:            "OCI repo with empty host falls through to missing tag error",
			versionOverride: "",
			helmRepo:        "oci://",
			expectError:     true,
			errorContains:   "either helm repository tag or spec.Version has to be set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, err := getVersion(tt.versionOverride, tt.helmRepo)
			if tt.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedVersion, version)
			}
		})
	}
}

func TestConstructTLSOptions(t *testing.T) {
	t.Run("no options returns no error", func(t *testing.T) {
		tlsOpt, err := constructTLSOptions(t.TempDir())
		require.NoError(t, err)
		assert.NotNil(t, tlsOpt)
	})

	t.Run("empty targetDir returns error", func(t *testing.T) {
		_, err := constructTLSOptions("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "target directory")
	})

	t.Run("nil credentials returns no error", func(t *testing.T) {
		tlsOpt, err := constructTLSOptions(t.TempDir(), withCredentials(nil))
		require.NoError(t, err)
		assert.NotNil(t, tlsOpt)
	})

	t.Run("CACertFile is used when set", func(t *testing.T) {
		tmpDir := t.TempDir()
		caFile := filepath.Join(tmpDir, "ca.pem")
		require.NoError(t, os.WriteFile(caFile, []byte("fake-ca-cert"), 0o600))

		tlsOpt, err := constructTLSOptions(tmpDir, withCACertFile(caFile))
		require.NoError(t, err)
		assert.NotNil(t, tlsOpt)
	})

	t.Run("CACert creates temp file", func(t *testing.T) {
		tmpDir := t.TempDir()
		caCert := "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----"
		tlsOpt, err := constructTLSOptions(tmpDir, withCACert(caCert))
		require.NoError(t, err)
		assert.NotNil(t, tlsOpt)

		// Verify a temp CA cert file was created in the target dir
		matches, err := filepath.Glob(filepath.Join(tmpDir, "caCert-*.pem"))
		require.NoError(t, err)
		assert.Len(t, matches, 1, "expected one temporary CA cert file")
	})

	t.Run("CACertFile takes precedence over CACert", func(t *testing.T) {
		tmpDir := t.TempDir()
		caFile := filepath.Join(tmpDir, "ca.pem")
		require.NoError(t, os.WriteFile(caFile, []byte("fake-ca-cert"), 0o600))
		_, err := constructTLSOptions(tmpDir, withCACertFile(caFile), withCACert("should-be-ignored"))
		require.NoError(t, err)

		// No temp file should be created since CACertFile was used
		matches, err := filepath.Glob(filepath.Join(tmpDir, "caCert-*.pem"))
		require.NoError(t, err)
		assert.Empty(t, matches, "no temp CA cert file should be created when CACertFile is set")
	})

	t.Run("certFile credential that does not exist returns error", func(t *testing.T) {
		_, err := constructTLSOptions(t.TempDir(), withCredentials(&helmcredsv1.HelmHTTPCredentials{
			CertFile: "/nonexistent/cert.pem",
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "certFile")
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("keyFile credential that does not exist returns error", func(t *testing.T) {
		_, err := constructTLSOptions(t.TempDir(), withCredentials(&helmcredsv1.HelmHTTPCredentials{
			KeyFile: "/nonexistent/key.pem",
		}))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keyFile")
		assert.Contains(t, err.Error(), "does not exist")
	})

	t.Run("valid certFile and keyFile credentials", func(t *testing.T) {
		tmpDir := t.TempDir()
		certFile := filepath.Join(tmpDir, "cert.pem")
		keyFile := filepath.Join(tmpDir, "key.pem")
		require.NoError(t, os.WriteFile(certFile, []byte("fake-cert"), 0o600))
		require.NoError(t, os.WriteFile(keyFile, []byte("fake-key"), 0o600))

		tlsOpt, err := constructTLSOptions(tmpDir, withCredentials(&helmcredsv1.HelmHTTPCredentials{
			CertFile: certFile,
			KeyFile:  keyFile,
		}))
		require.NoError(t, err)
		assert.NotNil(t, tlsOpt)
	})
}

func TestWithOCICredentials(t *testing.T) {
	t.Run("assigns OCI credentials to option", func(t *testing.T) {
		opt := &option{}
		creds := &ocicredsv1.OCICredentials{AccessToken: "tok-xyz"}
		WithOCICredentials(creds)(opt)
		assert.Same(t, creds, opt.OCICredentials)
	})

	t.Run("nil credentials are allowed and overwrite previous value", func(t *testing.T) {
		opt := &option{OCICredentials: &ocicredsv1.OCICredentials{AccessToken: "stale"}}
		WithOCICredentials(nil)(opt)
		assert.Nil(t, opt.OCICredentials)
	})
}

// TestNewReadOnlyChartFromRemote_BasicAuthAccessTokenFallback exercises the special
// credential resolution in [NewReadOnlyChartFromRemote]: when the helm HTTP credentials
// do not carry a password, the password is sourced from the OCI access token instead.
// The username is always sourced from the helm HTTP credentials and basic auth is only
// applied when both username and (resolved) password are non-empty.
func TestNewReadOnlyChartFromRemote_BasicAuthAccessTokenFallback(t *testing.T) {
	workDir, err := os.Getwd()
	require.NoError(t, err)
	testDataDir := filepath.Join(workDir, "..", "..", "testdata")

	tests := []struct {
		name       string
		helmCreds  *helmcredsv1.HelmHTTPCredentials
		ociCreds   *ocicredsv1.OCICredentials
		serverUser string
		serverPass string
		wantErr    bool
	}{
		{
			name:       "helm password is used when set",
			helmCreds:  &helmcredsv1.HelmHTTPCredentials{Username: "user1", Password: "pw1"},
			serverUser: "user1",
			serverPass: "pw1",
		},
		{
			name:       "OCI access token is used as password when helm password is empty",
			helmCreds:  &helmcredsv1.HelmHTTPCredentials{Username: "user1"},
			ociCreds:   &ocicredsv1.OCICredentials{AccessToken: "tok-123"},
			serverUser: "user1",
			serverPass: "tok-123",
		},
		{
			name:       "helm password takes precedence over OCI access token",
			helmCreds:  &helmcredsv1.HelmHTTPCredentials{Username: "user1", Password: "pw1"},
			ociCreds:   &ocicredsv1.OCICredentials{AccessToken: "ignored"},
			serverUser: "user1",
			serverPass: "pw1",
		},
		{
			name:       "missing username skips basic auth even when access token is set",
			helmCreds:  &helmcredsv1.HelmHTTPCredentials{},
			ociCreds:   &ocicredsv1.OCICredentials{AccessToken: "tok-123"},
			serverUser: "user1",
			serverPass: "tok-123",
			wantErr:    true,
		},
		{
			name:       "no credentials at all -> unauthorized",
			serverUser: "user1",
			serverPass: "pw1",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newBasicAuthChartServer(t, testDataDir, tt.serverUser, tt.serverPass)
			chartURL := srv.URL + "/mychart-0.1.0.tgz"

			var opts []Option
			if tt.helmCreds != nil {
				opts = append(opts, WithCredentials(tt.helmCreds))
			}
			if tt.ociCreds != nil {
				opts = append(opts, WithOCICredentials(tt.ociCreds))
			}

			chart, err := NewReadOnlyChartFromRemote(t.Context(), chartURL, t.TempDir(), opts...)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, chart)
			assert.Equal(t, "mychart", chart.Name)
			assert.Equal(t, "0.1.0", chart.Version)
		})
	}
}

// newBasicAuthChartServer returns an httptest server that serves files from dir
// only when the request carries matching HTTP Basic Auth credentials.
func newBasicAuthChartServer(t *testing.T, dir, user, pass string) *httptest.Server {
	t.Helper()
	fs := http.FileServer(http.Dir(dir))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != user || p != pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fs.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}
