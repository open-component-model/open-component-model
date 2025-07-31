package helm_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/input/helm"
	v1 "ocm.software/open-component-model/bindings/go/input/helm/spec/v1"
	"ocm.software/open-component-model/bindings/go/oci/tar"
)

func TestGetV1HelmBlob_ValidateFields(t *testing.T) {
	ctx := t.Context()

	tests := []struct {
		name     string
		helmSpec v1.Helm
	}{
		{
			name: "empty path",
			helmSpec: v1.Helm{
				Path: "",
			},
		},
		{
			name: "version set",
			helmSpec: v1.Helm{
				Path:    "path/to/chart",
				Version: "1.2.3",
			},
		},
		{
			name: "caCert set",
			helmSpec: v1.Helm{
				Path:   "path/to/chart",
				CACert: "caCert",
			},
		},
		{
			name: "caCertFile set",
			helmSpec: v1.Helm{
				Path:       "path/to/chart",
				CACertFile: "caCertFile",
			},
		},
		{
			name: "Repository set",
			helmSpec: v1.Helm{
				Path:       "path/to/chart",
				Repository: "repository",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, err := helm.GetV1HelmBlob(ctx, tt.helmSpec, "")
			require.Error(t, err)
			assert.True(t, func() bool {
				return errors.Is(err, helm.ErrEmptyPath) || errors.Is(err, helm.ErrUnsupportedField)
			}(), "Expected ErrEmptyPath or ErrUnsupportedField, got: %v", err)
			assert.Nil(t, b, "expected nil blob for invalid helm spec")
		})
	}
}

func TestGetV1HelmBlob_Success(t *testing.T) {
	ctx := t.Context()
	workDir, err := os.Getwd()
	require.NoError(t, err, "failed to get current working directory")
	testDataDir := filepath.Join(workDir, "testdata")

	tests := []struct {
		name string
		path string
	}{
		{
			name: "non-packaged helm chart",
			path: filepath.Join(testDataDir, "mychart"),
		},
		{
			name: "packaged helm chart",
			path: filepath.Join(testDataDir, "mychart-0.1.0.tgz"),
		},
		// TODO: packaged with provenance file
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := v1.Helm{
				Path: tt.path,
			}
			b, err := helm.GetV1HelmBlob(ctx, spec, "")
			require.NoError(t, err)
			require.NotNil(t, b)

			store, err := tar.ReadOCILayout(ctx, b)
			require.NoError(t, err)
			require.NotNil(t, store)
			t.Cleanup(func() {
				require.NoError(t, store.Close())
			})
			require.Len(t, store.Index.Manifests, 1)
		})
	}
}

func TestGetV1HelmBlob_BadCharts(t *testing.T) {
	ctx := t.Context()
	workDir, err := os.Getwd()
	require.NoError(t, err, "failed to get current working directory")
	testDataDir := filepath.Join(workDir, "testdata")

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
			b, err := helm.GetV1HelmBlob(ctx, spec, "")
			require.Error(t, err)
			require.Nil(t, b)
			assert.Contains(t, err.Error(), tt.wantErrMgs)
		})
	}
}
