package internal

import (
	"testing"

	"github.com/stretchr/testify/require"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestShouldUploadAsOCIArtifact(t *testing.T) {
	ociTarget := &oci.Repository{
		Type:    runtime.Type{Name: "OCIRepository", Version: "v1"},
		BaseUrl: "http://localhost:5000",
	}
	ctfTarget := &ctfv1.Repository{
		Type:     runtime.Type{Name: "CommonTransportFormat", Version: "v1"},
		FilePath: "/tmp/test-ctf",
	}

	tests := []struct {
		name       string
		uploadType UploadType
		toSpec     runtime.Typed
		want       bool
	}{
		{
			name:       "explicit ociArtifact with OCI target",
			uploadType: UploadAsOciArtifact,
			toSpec:     ociTarget,
			want:       true,
		},
		{
			name:       "explicit ociArtifact with CTF target",
			uploadType: UploadAsOciArtifact,
			toSpec:     ctfTarget,
			want:       true,
		},
		{
			name:       "explicit localBlob with OCI target",
			uploadType: UploadAsLocalBlob,
			toSpec:     ociTarget,
			want:       false,
		},
		{
			name:       "explicit localBlob with CTF target",
			uploadType: UploadAsLocalBlob,
			toSpec:     ctfTarget,
			want:       false,
		},
		{
			name:       "default with OCI target uploads as OCI artifact",
			uploadType: UploadAsDefault,
			toSpec:     ociTarget,
			want:       true,
		},
		{
			name:       "default with CTF target uploads as local blob",
			uploadType: UploadAsDefault,
			toSpec:     ctfTarget,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got := shouldUploadAsOCIArtifact(tt.uploadType, tt.toSpec)
			r.Equal(tt.want, got)
		})
	}
}

func TestValidateUploadType(t *testing.T) {
	ociTarget := &oci.Repository{
		Type:    runtime.Type{Name: "OCIRepository", Version: "v1"},
		BaseUrl: "http://localhost:5000",
	}
	ctfTarget := &ctfv1.Repository{
		Type:     runtime.Type{Name: "CommonTransportFormat", Version: "v1"},
		FilePath: "/tmp/test-ctf",
	}

	tests := []struct {
		name       string
		uploadType UploadType
		toSpec     runtime.Typed
		wantErr    bool
	}{
		{
			name:       "ociArtifact with CTF target is not allowed",
			uploadType: UploadAsOciArtifact,
			toSpec:     ctfTarget,
			wantErr:    true,
		},
		{
			name:       "ociArtifact with OCI target is allowed",
			uploadType: UploadAsOciArtifact,
			toSpec:     ociTarget,
			wantErr:    false,
		},
		{
			name:       "localBlob with CTF target is allowed",
			uploadType: UploadAsLocalBlob,
			toSpec:     ctfTarget,
			wantErr:    false,
		},
		{
			name:       "localBlob with OCI target is allowed",
			uploadType: UploadAsLocalBlob,
			toSpec:     ociTarget,
			wantErr:    false,
		},
		{
			name:       "default with CTF target is allowed",
			uploadType: UploadAsDefault,
			toSpec:     ctfTarget,
			wantErr:    false,
		},
		{
			name:       "default with OCI target is allowed",
			uploadType: UploadAsDefault,
			toSpec:     ociTarget,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			err := validateUploadType(tt.uploadType, tt.toSpec)
			if tt.wantErr {
				r.Error(err)
				r.Contains(err.Error(), "cannot upload as OCI artifact to a CTF archive")
			} else {
				r.NoError(err)
			}
		})
	}
}

func TestGetReferenceName(t *testing.T) {
	tests := []struct {
		name          string
		ociImage      ociv1.OCIImage
		wantReference string
		wantErr       bool
		errContains   string
	}{
		// Valid OCI references
		{
			name: "valid oci reference with scheme and path",
			ociImage: ociv1.OCIImage{
				ImageReference: "oci://registry.io/path/to/image:tag",
			},
			wantReference: "path/to/image:tag",
			wantErr:       false,
		},
		{
			name: "valid https reference",
			ociImage: ociv1.OCIImage{
				ImageReference: "https://registry.io/path/to/image",
			},
			wantReference: "path/to/image",
			wantErr:       false,
		},
		{
			name: "valid http reference",
			ociImage: ociv1.OCIImage{
				ImageReference: "http://registry.io/path/to/image",
			},
			wantReference: "path/to/image",
			wantErr:       false,
		},
		{
			name: "valid path only reference",
			ociImage: ociv1.OCIImage{
				ImageReference: "/path/to/image",
			},
			wantReference: "path/to/image",
			wantErr:       false,
		},
		{
			name: "valid reference with port",
			ociImage: ociv1.OCIImage{
				ImageReference: "oci://registry.io:5000/path/to/image",
			},
			wantReference: "path/to/image",
			wantErr:       false,
		},
		{
			name: "valid reference with digest",
			ociImage: ociv1.OCIImage{
				ImageReference: "oci://registry.io/image@sha256:d49cede63746a2d5a7de9f8b13937966e5bddd2bb8e36100d852f71c7e282351",
			},
			wantReference: "image",
			wantErr:       false,
		},
		{
			name: "valid reference with tag and digest",
			ociImage: ociv1.OCIImage{
				ImageReference: "oci://registry.io/image:v1.0.0@sha256:d49cede63746a2d5a7de9f8b13937966e5bddd2bb8e36100d852f71c7e282351",
			},
			wantReference: "image:v1.0.0",
			wantErr:       false,
		},
		{
			name: "valid reference with multiple path segments",
			ociImage: ociv1.OCIImage{
				ImageReference: "oci://registry.io/org/project/component/image:ociv1.0.0",
			},
			wantReference: "org/project/component/image:ociv1.0.0",
			wantErr:       false,
		},
		// Invalid OCI references
		{
			name: "invalid reference with query parameters",
			ociImage: ociv1.OCIImage{
				ImageReference: "oci://registry.io/path/to/image?param=value",
			},
			wantReference: "",
			wantErr:       true,
		},
		{
			name: "invalid reference with fragment",
			ociImage: ociv1.OCIImage{
				ImageReference: "oci://registry.io/path/to/image#fragment",
			},
			wantReference: "",
			wantErr:       true,
		},
		{
			name: "invalid empty image reference",
			ociImage: ociv1.OCIImage{
				ImageReference: "",
			},
			wantReference: "",
			wantErr:       true,
		},
		{
			name: "invalid reference with invalid characters in scheme",
			ociImage: ociv1.OCIImage{
				ImageReference: "ht!tp://registry.io/path/to/image",
			},
			wantReference: "",
			wantErr:       true,
			errContains:   "invalid OCI image reference",
		},
		{
			name: "invalid reference with control characters",
			ociImage: ociv1.OCIImage{
				ImageReference: "oci://registry.io/path\x00/image",
			},
			wantReference: "",
			wantErr:       true,
			errContains:   "invalid OCI image reference",
		},
		{
			name: "invalid reference with backslashes",
			ociImage: ociv1.OCIImage{
				ImageReference: "oci://registry.io\\path\\to\\image",
			},
			wantReference: "",
			wantErr:       true,
			errContains:   "invalid OCI image reference",
		},
		{
			name: "invalid reference with newline",
			ociImage: ociv1.OCIImage{
				ImageReference: "oci://registry.io/path\n/image",
			},
			wantReference: "",
			wantErr:       true,
			errContains:   "invalid OCI image reference",
		},
		{
			name: "invalid reference with tab character",
			ociImage: ociv1.OCIImage{
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

			gotReference, gotErr := GetReferenceName(tt.ociImage)

			if tt.wantErr {
				r.Error(gotErr, "GetReferenceName() should return an error")
				if tt.errContains != "" {
					r.ErrorContains(gotErr, tt.errContains, "error message should contain expected text")
				}
			} else {
				r.NoError(gotErr, "GetReferenceName() should not return an error")
			}

			r.Equal(tt.wantReference, gotReference, "GetReferenceName() returned unexpected reference")
		})
	}
}
