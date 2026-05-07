package download

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
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
