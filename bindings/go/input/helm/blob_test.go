package helm_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/input/helm"
	v1 "ocm.software/open-component-model/bindings/go/input/helm/spec/v1"
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
			b, err := helm.GetV1HelmBlob(ctx, tt.helmSpec)
			require.Error(t, err)
			assert.True(t, func() bool {
				return errors.Is(err, helm.ErrEmptyPath) || errors.Is(err, helm.ErrUnsupportedField)
			}(), "Expected ErrEmptyPath or ErrUnsupportedField, got: %v", err)
			assert.Nil(t, b, "expected nil blob for invalid helm spec")
		})
	}
}
