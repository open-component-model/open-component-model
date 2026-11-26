package v1

import (
	"encoding/json/v2"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
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
			name:  "registry without tag or digest",
			layer: OCIImageLayer{Reference: "example.com/repo", Digest: validDigest, Size: 100},
		},
		{
			name:  "scheme with digest",
			layer: OCIImageLayer{Reference: "https://example.com/repo@" + validDigest.String(), Digest: validDigest, Size: 100},
		},
		{
			name:    "tag",
			layer:   OCIImageLayer{Reference: "example.com/repo:latest", Digest: validDigest, Size: 100},
			wantErr: true,
		},
		{
			name:    "tag with digest",
			layer:   OCIImageLayer{Reference: "https://example.com/repo:latest@" + validDigest.String(), Digest: validDigest, Size: 100},
			wantErr: true,
		},
		{
			name:  "manifest reference in the same repository",
			layer: OCIImageLayer{Reference: "example.com/repo", Digest: validDigest, Size: 100, ManifestReference: "example.com/repo:1.0.0@" + digest.FromString("manifest").String()},
		},
		{
			name:    "manifest reference in another repository",
			layer:   OCIImageLayer{Reference: "example.com/repo", Digest: validDigest, Size: 100, ManifestReference: "example.com/other:1.0.0"},
			wantErr: true,
		},
		{
			name:    "invalid manifest reference",
			layer:   OCIImageLayer{Reference: "example.com/repo", Digest: validDigest, Size: 100, ManifestReference: "example.com/repo@sha256:nope"},
			wantErr: true,
		},
		{
			name:  "zero size",
			layer: OCIImageLayer{Reference: "example.com/repo", Digest: digest.FromString(""), Size: 0},
		},
		{
			name:  "reference without registry",
			layer: OCIImageLayer{Reference: "nginx", Digest: validDigest, Size: 100},
		},
		{
			name:  "digest reference without registry",
			layer: OCIImageLayer{Reference: "nginx@" + validDigest.String(), Digest: validDigest, Size: 100},
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
			r := require.New(t)
			err := tt.layer.Validate()
			if tt.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
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
		{name: "leading slash", reference: "/ocm/value:v2.0", wantErr: true},
		{name: "scheme", reference: "oci://ocm/value:v2.0", wantErr: true},
		{name: "host:port", reference: "registry.io:5000/value:v2.0", wantErr: true},
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

// The OCM spec reserves imageReference for the repository of an ociBlob access, so the
// image that lists the layer is written under manifestReference. A layer without it must
// serialise exactly as before the field existed, or stored descriptors change.
func TestOCIImageLayer_ManifestReferenceJSON(t *testing.T) {
	dig := digest.FromString("test")
	for _, tt := range []struct {
		name     string
		manifest string
		wantJSON string
	}{
		{
			name:     "without manifest reference",
			wantJSON: `{"type":"ociBlob/v1","ref":"example.com/repo","digest":"` + dig.String() + `","size":100}`,
		},
		{
			name:     "with manifest reference",
			manifest: "example.com/repo:1.0.0",
			wantJSON: `{"type":"ociBlob/v1","ref":"example.com/repo","digest":"` + dig.String() + `","size":100,"manifestReference":"example.com/repo:1.0.0"}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			layer := OCIImageLayer{
				Type:              runtime.NewVersionedType(LegacyOCIBlobAccessType, LegacyOCIBlobAccessTypeVersion),
				Reference:         "example.com/repo",
				Digest:            dig,
				Size:              100,
				ManifestReference: tt.manifest,
			}
			data, err := json.Marshal(&layer, runtime.JSONOptions)
			r.NoError(err)
			r.JSONEq(tt.wantJSON, string(data))

			var decoded OCIImageLayer
			r.NoError(json.Unmarshal(data, &decoded, runtime.JSONOptions))
			r.Equal(layer, decoded)
		})
	}
}
