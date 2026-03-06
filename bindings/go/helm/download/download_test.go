package download

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	t.Run("empty options returns no error", func(t *testing.T) {
		opts := &option{
			TargetDir:   t.TempDir(),
			Credentials: make(map[string]string),
		}
		tlsOpt, clearCache, err := constructTLSOptions(opts)
		require.NoError(t, err)
		assert.NotNil(t, tlsOpt)
		assert.NotNil(t, clearCache)
		require.NoError(t, clearCache())
	})

	t.Run("nil credentials returns no error", func(t *testing.T) {
		opts := &option{
			TargetDir: t.TempDir(),
		}
		tlsOpt, clearCache, err := constructTLSOptions(opts)
		require.NoError(t, err)
		assert.NotNil(t, tlsOpt)
		require.NoError(t, clearCache())
	})

	t.Run("CACertFile is used when set", func(t *testing.T) {
		tmpDir := t.TempDir()
		caFile := filepath.Join(tmpDir, "ca.pem")
		require.NoError(t, os.WriteFile(caFile, []byte("fake-ca-cert"), 0o600))

		opts := &option{
			TargetDir:  tmpDir,
			CACertFile: caFile,
		}
		tlsOpt, clearCache, err := constructTLSOptions(opts)
		require.NoError(t, err)
		assert.NotNil(t, tlsOpt)
		require.NoError(t, clearCache())
	})

	t.Run("CACert creates temp file", func(t *testing.T) {
		tmpDir := t.TempDir()
		opts := &option{
			TargetDir: tmpDir,
			CACert:    "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----",
		}
		tlsOpt, clearCache, err := constructTLSOptions(opts)
		require.NoError(t, err)
		assert.NotNil(t, tlsOpt)

		// Verify a temp CA cert file was created in the target dir
		matches, err := filepath.Glob(filepath.Join(tmpDir, "caCert-*.pem"))
		require.NoError(t, err)
		assert.Len(t, matches, 1, "expected one temporary CA cert file")

		// clearCache should remove the temp file
		require.NoError(t, clearCache())
		_, err = os.Stat(matches[0])
		assert.True(t, os.IsNotExist(err), "temp CA cert file should be removed after clearCache")
	})

	t.Run("CACertFile takes precedence over CACert", func(t *testing.T) {
		tmpDir := t.TempDir()
		caFile := filepath.Join(tmpDir, "ca.pem")
		require.NoError(t, os.WriteFile(caFile, []byte("fake-ca-cert"), 0o600))

		opts := &option{
			TargetDir:  tmpDir,
			CACertFile: caFile,
			CACert:     "should-be-ignored",
		}
		_, clearCache, err := constructTLSOptions(opts)
		require.NoError(t, err)

		// No temp file should be created since CACertFile was used
		matches, err := filepath.Glob(filepath.Join(tmpDir, "caCert-*.pem"))
		require.NoError(t, err)
		assert.Empty(t, matches, "no temp CA cert file should be created when CACertFile is set")
		require.NoError(t, clearCache())
	})

	t.Run("certFile credential that does not exist returns error", func(t *testing.T) {
		opts := &option{
			TargetDir: t.TempDir(),
			Credentials: map[string]string{
				CredentialCertFile: "/nonexistent/cert.pem",
			},
		}
		_, clearCache, err := constructTLSOptions(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "certFile")
		assert.Contains(t, err.Error(), "does not exist")
		require.NoError(t, clearCache())
	})

	t.Run("keyFile credential that does not exist returns error", func(t *testing.T) {
		opts := &option{
			TargetDir: t.TempDir(),
			Credentials: map[string]string{
				CredentialKeyFile: "/nonexistent/key.pem",
			},
		}
		_, clearCache, err := constructTLSOptions(opts)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keyFile")
		assert.Contains(t, err.Error(), "does not exist")
		require.NoError(t, clearCache())
	})

	t.Run("valid certFile and keyFile credentials", func(t *testing.T) {
		tmpDir := t.TempDir()
		certFile := filepath.Join(tmpDir, "cert.pem")
		keyFile := filepath.Join(tmpDir, "key.pem")
		require.NoError(t, os.WriteFile(certFile, []byte("fake-cert"), 0o600))
		require.NoError(t, os.WriteFile(keyFile, []byte("fake-key"), 0o600))

		opts := &option{
			TargetDir: tmpDir,
			Credentials: map[string]string{
				CredentialCertFile: certFile,
				CredentialKeyFile:  keyFile,
			},
		}
		tlsOpt, clearCache, err := constructTLSOptions(opts)
		require.NoError(t, err)
		assert.NotNil(t, tlsOpt)
		require.NoError(t, clearCache())
	})
}

func TestGetterProviders(t *testing.T) {
	providers := getterProviders()
	require.Len(t, providers, 2, "expected two getter providers")

	assert.Contains(t, providers[0].Schemes, "http")
	assert.Contains(t, providers[0].Schemes, "https")
	assert.Contains(t, providers[1].Schemes, "oci")
}

func TestOptions(t *testing.T) {
	t.Run("WithVersion", func(t *testing.T) {
		opt := &option{}
		WithVersion("1.0.0")(opt)
		assert.Equal(t, "1.0.0", opt.Version)
	})

	t.Run("WithCACert", func(t *testing.T) {
		opt := &option{}
		WithCACert("cert-data")(opt)
		assert.Equal(t, "cert-data", opt.CACert)
	})

	t.Run("WithCACertFile", func(t *testing.T) {
		opt := &option{}
		WithCACertFile("/path/to/ca.pem")(opt)
		assert.Equal(t, "/path/to/ca.pem", opt.CACertFile)
	})

	t.Run("WithTempDirBase", func(t *testing.T) {
		opt := &option{}
		WithTempDirBase("/tmp/custom")(opt)
		assert.Equal(t, "/tmp/custom", opt.TargetDir)
	})

	t.Run("WithCredentials", func(t *testing.T) {
		opt := &option{}
		creds := map[string]string{"username": "user", "password": "pass"}
		WithCredentials(creds)(opt)
		assert.Equal(t, creds, opt.Credentials)
	})

	t.Run("WithAlwaysDownloadProv", func(t *testing.T) {
		opt := &option{}
		WithAlwaysDownloadProv(true)(opt)
		assert.True(t, opt.AlwaysDownloadProv)
	})
}
