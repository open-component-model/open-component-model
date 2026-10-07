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

func TestRelativeOCIReference_Validate(t *testing.T) {
	for _, tt := range []struct {
		name      string
		reference string
		wantErr   bool
	}{
		{name: "tag only", reference: "ocm/value:v2.0"},
		{name: "single segment", reference: "value:v2.0"},
		{name: "multi-segment", reference: "ocm-prefix/images/app:v1"},
		{name: "dotted first segment is a path", reference: "acme.org/value:v2.0"},
		{name: "digest only", reference: "ocm/value@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},
		{name: "tag and digest", reference: "ocm/value:v2.0@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"},

		{name: "empty", reference: "", wantErr: true},
		{name: "invalid syntax", reference: "ocm/value:bad:tag:form", wantErr: true},
		{name: "invalid digest", reference: "ocm/value@sha256:nothex", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := (&RelativeOCIReference{Reference: tt.reference}).Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
