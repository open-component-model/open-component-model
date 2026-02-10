package internal

import (
	"testing"

	"github.com/stretchr/testify/require"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
)

func TestGetImageReference(t *testing.T) {
	tests := []struct {
		name          string
		ociImage      v1.OCIImage
		wantReference string
		wantErr       bool
		errContains   string
	}{
		// Valid OCI references
		{
			name: "valid oci reference with scheme and path",
			ociImage: v1.OCIImage{
				ImageReference: "oci://registry.io/path/to/image:tag",
			},
			wantReference: "/path/to/image:tag",
			wantErr:       false,
		},
		{
			name: "valid https reference",
			ociImage: v1.OCIImage{
				ImageReference: "https://registry.io/path/to/image",
			},
			wantReference: "/path/to/image",
			wantErr:       false,
		},
		{
			name: "valid http reference",
			ociImage: v1.OCIImage{
				ImageReference: "http://registry.io/path/to/image",
			},
			wantReference: "/path/to/image",
			wantErr:       false,
		},
		{
			name: "valid path only reference",
			ociImage: v1.OCIImage{
				ImageReference: "/path/to/image",
			},
			wantReference: "/path/to/image",
			wantErr:       false,
		},
		{
			name: "valid reference with port",
			ociImage: v1.OCIImage{
				ImageReference: "oci://registry.io:5000/path/to/image",
			},
			wantReference: "/path/to/image",
			wantErr:       false,
		},
		{
			name: "valid reference with query parameters",
			ociImage: v1.OCIImage{
				ImageReference: "oci://registry.io/path/to/image?param=value",
			},
			wantReference: "/path/to/image",
			wantErr:       false,
		},
		{
			name: "valid reference with fragment",
			ociImage: v1.OCIImage{
				ImageReference: "oci://registry.io/path/to/image#fragment",
			},
			wantReference: "/path/to/image",
			wantErr:       false,
		},
		{
			name: "valid reference with multiple path segments",
			ociImage: v1.OCIImage{
				ImageReference: "oci://registry.io/org/project/component/image:v1.0.0",
			},
			wantReference: "/org/project/component/image:v1.0.0",
			wantErr:       false,
		},
		{
			name: "empty image reference",
			ociImage: v1.OCIImage{
				ImageReference: "",
			},
			wantReference: "",
			wantErr:       false,
		},
		// Invalid OCI references
		{
			name: "invalid reference with invalid characters in scheme",
			ociImage: v1.OCIImage{
				ImageReference: "ht!tp://registry.io/path/to/image",
			},
			wantReference: "",
			wantErr:       true,
			errContains:   "invalid OCI image reference",
		},
		{
			name: "invalid reference with control characters",
			ociImage: v1.OCIImage{
				ImageReference: "oci://registry.io/path\x00/image",
			},
			wantReference: "",
			wantErr:       true,
			errContains:   "invalid OCI image reference",
		},
		{
			name: "invalid reference with backslashes",
			ociImage: v1.OCIImage{
				ImageReference: "oci://registry.io\\path\\to\\image",
			},
			wantReference: "",
			wantErr:       true,
			errContains:   "invalid OCI image reference",
		},
		{
			name: "invalid reference with newline",
			ociImage: v1.OCIImage{
				ImageReference: "oci://registry.io/path\n/image",
			},
			wantReference: "",
			wantErr:       true,
			errContains:   "invalid OCI image reference",
		},
		{
			name: "invalid reference with tab character",
			ociImage: v1.OCIImage{
				ImageReference: "oci://registry.io/path\t/image",
			},
			wantReference: "",
			wantErr:       true,
			errContains:   "invalid OCI image reference",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			gotReference, gotErr := GetImageReference(tt.ociImage)

			if tt.wantErr {
				r.Error(gotErr, "GetImageReference() should return an error")
				if tt.errContains != "" {
					r.ErrorContains(gotErr, tt.errContains, "error message should contain expected text")
				}
			} else {
				r.NoError(gotErr, "GetImageReference() should not return an error")
			}

			r.Equal(tt.wantReference, gotReference, "GetImageReference() returned unexpected reference")
		})
	}
}

func TestGetImageReference_ExtractsPathCorrectly(t *testing.T) {
	tests := []struct {
		name          string
		imageRef      string
		expectedPath  string
	}{
		{
			name:         "extracts path from full OCI URL",
			imageRef:     "oci://ghcr.io/open-component-model/ocm:latest",
			expectedPath: "/open-component-model/ocm:latest",
		},
		{
			name:         "extracts path ignoring host",
			imageRef:     "https://example.com/my/image",
			expectedPath: "/my/image",
		},
		{
			name:         "extracts path with version tag",
			imageRef:     "oci://registry.local/namespace/image:v1.2.3",
			expectedPath: "/namespace/image:v1.2.3",
		},
		{
			name:         "extracts path with digest",
			imageRef:     "oci://registry.io/image@sha256:abc123",
			expectedPath: "/image@sha256:abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			ociImage := v1.OCIImage{
				ImageReference: tt.imageRef,
			}

			gotPath, err := GetImageReference(ociImage)
			r.NoError(err)
			r.Equal(tt.expectedPath, gotPath)
		})
	}
}
