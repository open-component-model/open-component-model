package v1

import (
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOCIImageLayer_Validate(t *testing.T) {
	validDigest := digest.FromString("test")
	validRef := "example.com/repo@" + validDigest.String()

	tests := []struct {
		name    string
		layer   OCIImageLayer
		wantErr bool
	}{
		{
			name: "valid layer",
			layer: OCIImageLayer{
				Reference: validRef,
				Digest:    validDigest,
				Size:      100,
			},
			wantErr: false,
		},
		{
			name: "empty reference",
			layer: OCIImageLayer{
				Reference: "",
				Digest:    validDigest,
				Size:      100,
			},
			wantErr: true,
		},
		{
			name: "invalid digest",
			layer: OCIImageLayer{
				Reference: validRef,
				Digest:    "invalid-digest",
				Size:      100,
			},
			wantErr: true,
		},
		{
			name: "negative size",
			layer: OCIImageLayer{
				Reference: validRef,
				Digest:    validDigest,
				Size:      -1,
			},
			wantErr: true,
		},
		{
			name: "mismatched digest in reference",
			layer: OCIImageLayer{
				Reference: "example.com/repo@" + digest.FromString("different").String(),
				Digest:    validDigest,
				Size:      100,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.layer.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestOCIImageValidate(t *testing.T) {
	for _, tt := range []struct {
		name    string
		access  OCIImage
		wantErr string
	}{
		{
			name:   "valid",
			access: OCIImage{ImageReference: "ghcr.io/open-component-model/image:1.0.0"},
		},
		{
			name:    "empty image reference",
			access:  OCIImage{},
			wantErr: "imageReference is required",
		},
		{
			name:    "malformed image reference",
			access:  OCIImage{ImageReference: "ghcr.io/open-component-model/image:not a tag"},
			wantErr: "invalid imageReference",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.access.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}
