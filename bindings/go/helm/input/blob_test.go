package input_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/provenance"
	"helm.sh/helm/v4/pkg/registry"

	"ocm.software/open-component-model/bindings/go/helm/input"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	v1 "ocm.software/open-component-model/bindings/go/helm/spec/input/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

func TestGetV1HelmBlob_ValidateFields(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name        string
		helmSpec    v1.Helm
		expectError bool
	}{
		{
			name: "empty path and repository",
			helmSpec: v1.Helm{
				Path:           "",
				HelmRepository: "",
			},
			expectError: true,
		},
		{
			name: "path set - should work",
			helmSpec: v1.Helm{
				Path: "path/to/chart",
			},
			expectError: false, // Will fail later due to missing file, but validation should pass
		},
		{
			name: "repository set - should work",
			helmSpec: v1.Helm{
				HelmRepository: "https://charts.example.com",
			},
			expectError: false, // Will fail later due to network, but validation should pass
		},
		{
			name: "both path and repository set - should fail",
			helmSpec: v1.Helm{
				Path:           "path/to/chart",
				HelmRepository: "https://charts.example.com",
			},
			expectError: true, // Both fields are mutually exclusive
		},
		{
			name: "all remote fields set - should work",
			helmSpec: v1.Helm{
				HelmRepository: "https://charts.example.com",
				Version:        "1.2.3",
				CACert:         "caCert",
				CACertFile:     "caCertFile",
				Repository:     "oci://example.com/repo",
			},
			expectError: false, // All fields now supported
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, _, err := input.GetV1HelmBlob(ctx, tt.helmSpec, "")
			if tt.expectError {
				require.Error(t, err)
				assert.Nil(t, b, "expected nil blob for invalid helm spec")
			} else {
				// error is ok if it's not a validation error
				if err != nil {
					assert.NotContains(t, err.Error(), "either path or helmRepository must be specified")
				}
			}
		})
	}
}

func TestGetV1HelmBlob_Success(t *testing.T) {
	ctx := t.Context()
	workDir, err := os.Getwd()
	require.NoError(t, err, "failed to get current working directory")
	testDataDir := filepath.Join(workDir, "../testdata")

	tests := []struct {
		name      string
		path      string
		provGPG   string
		provKeyID string
	}{
		{
			name: "non-packaged helm chart",
			path: filepath.Join(testDataDir, "mychart"),
		},
		{
			name: "packaged helm chart",
			path: filepath.Join(testDataDir, "mychart-0.1.0.tgz"),
		},
		{
			name: "packaged helm chart with provenance file",
			path: filepath.Join(testDataDir, "provenance", "mychart-0.1.0.tgz"),
			// this public key is used to verify the provenance file and contains a static, non expiring
			// RSA key for testing purposes.
			provGPG:   filepath.Join(testDataDir, "provenance", "pub.gpg"),
			provKeyID: "testkey",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := v1.Helm{
				Path: tt.path,
			}
			b, _, err := input.GetV1HelmBlob(ctx, spec, "")
			require.NoError(t, err)
			require.NotNil(t, b)

			store, err := tar.ReadOCILayout(ctx, b)
			require.NoError(t, err)
			require.NotNil(t, store)
			t.Cleanup(func() {
				require.NoError(t, store.Close())
			})
			require.Len(t, store.Index.Manifests, 1)

			manifestRaw, err := store.Fetch(ctx, store.Index.Manifests[0])
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, manifestRaw.Close())
			})
			manifest := ociImageSpecV1.Manifest{}
			require.NoError(t, json.NewDecoder(manifestRaw).Decode(&manifest))

			require.GreaterOrEqual(t, len(manifest.Layers), 1, "expected at least one layer")
			require.Equal(t, registry.ChartLayerMediaType, manifest.Layers[0].MediaType, "expected first layer to be chart layer")

			if tt.provGPG != "" {
				signatory, err := provenance.NewFromKeyring(tt.provGPG, tt.provKeyID)
				require.NoError(t, err, "failed to create signatory from GPG keyring")

				var provFile string
				t.Run("provenance verification", func(t *testing.T) {
					require.Len(t, manifest.Layers, 2, "expected two layers for chart and provenance file")
					require.Equal(t, registry.ProvLayerMediaType, manifest.Layers[1].MediaType, "expected second layer to be provenance file")

					chartLayer, err := store.Fetch(ctx, manifest.Layers[0])
					require.NoError(t, err)
					t.Cleanup(func() {
						require.NoError(t, chartLayer.Close())
					})

					chartData, err := io.ReadAll(chartLayer)
					require.NoError(t, err, "failed to read chart layer")

					provLayer, err := store.Fetch(ctx, manifest.Layers[1])
					require.NoError(t, err)
					t.Cleanup(func() {
						require.NoError(t, provLayer.Close())
					})

					provData, err := io.ReadAll(provLayer)
					require.NoError(t, err, "failed to read provenance layer")

					// store the provenance data in a temporary file to use with HELM Verification library
					provFile = filepath.Join(t.TempDir(), "provenance.json")
					require.NoError(t, os.WriteFile(provFile, provData, 0o644))

					_, err = signatory.Verify(chartData, provData, filepath.Base(tt.path))
					require.NoError(t, err, "failed to verify provenance file")
				})
			}
		})
	}
}

func TestOptions(t *testing.T) {
	t.Run("WithCredentials sets helm http credentials", func(t *testing.T) {
		options := &input.Options{}
		creds := &helmcredsv1.HelmHTTPCredentials{Username: "u", Password: "p"}
		input.WithCredentials(creds)(options)
		assert.Same(t, creds, options.Credentials)
	})

	t.Run("WithOCICredentials sets OCI credentials", func(t *testing.T) {
		options := &input.Options{}
		creds := &ocicredsv1.OCICredentials{AccessToken: "tok"}
		input.WithOCICredentials(creds)(options)
		assert.Same(t, creds, options.OCICredentials)
	})

	t.Run("WithOCICredentials and WithCredentials coexist", func(t *testing.T) {
		options := &input.Options{}
		helmCreds := &helmcredsv1.HelmHTTPCredentials{Username: "u"}
		ociCreds := &ocicredsv1.OCICredentials{AccessToken: "tok"}
		input.WithCredentials(helmCreds)(options)
		input.WithOCICredentials(ociCreds)(options)
		assert.Same(t, helmCreds, options.Credentials)
		assert.Same(t, ociCreds, options.OCICredentials)
	})
}

// TestGetV1HelmBlob_OCICredentialsBasicAuthFallback verifies that an OCI access token
// supplied via [input.WithOCICredentials] is propagated through the download pipeline
// and used as the basic-auth password when no helm HTTP password is set.
func TestGetV1HelmBlob_OCICredentialsBasicAuthFallback(t *testing.T) {
	workDir, err := os.Getwd()
	require.NoError(t, err)
	testDataDir := filepath.Join(workDir, "..", "testdata")

	const (
		expectedUser = "user1"
		accessToken  = "tok-abc"
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != expectedUser || p != accessToken {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		http.FileServer(http.Dir(testDataDir)).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)

	spec := v1.Helm{
		HelmRepository: srv.URL + "/mychart-0.1.0.tgz",
	}

	b, chart, err := input.GetV1HelmBlob(t.Context(), spec, t.TempDir(),
		input.WithCredentials(&helmcredsv1.HelmHTTPCredentials{Username: expectedUser}),
		input.WithOCICredentials(&ocicredsv1.OCICredentials{AccessToken: accessToken}),
	)
	require.NoError(t, err)
	require.NotNil(t, b)
	require.NotNil(t, chart)
	assert.Equal(t, "mychart", chart.Name)
	assert.Equal(t, "0.1.0", chart.Version)
}

func TestGetV1HelmBlob_BadCharts(t *testing.T) {
	ctx := t.Context()
	workDir, err := os.Getwd()
	require.NoError(t, err, "failed to get current working directory")
	testDataDir := filepath.Join(workDir, "../testdata")

	tests := []struct {
		name       string
		path       string
		wantErrMgs string
	}{
		{
			name:       "bad chart version missing",
			path:       filepath.Join(testDataDir, "badchart"),
			wantErrMgs: "chart.metadata.version is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := v1.Helm{
				Path: tt.path,
			}
			b, _, err := input.GetV1HelmBlob(ctx, spec, "")
			require.Error(t, err)
			require.Nil(t, b)
			assert.Contains(t, err.Error(), tt.wantErrMgs)
		})
	}
}
