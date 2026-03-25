package internal

import (
	"testing"

	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	ociv1alpha1 "ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestConvertToConcreteRepo_OCIPassthrough(t *testing.T) {
	repo := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
	}
	result, err := convertToConcreteRepo(repo)
	require.NoError(t, err)
	assert.Equal(t, repo, result)
}

func TestConvertToConcreteRepo_CTFPassthrough(t *testing.T) {
	repo := &ctfv1.Repository{
		Type:     runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version},
		FilePath: "/tmp/archive",
	}
	result, err := convertToConcreteRepo(repo)
	require.NoError(t, err)
	assert.Equal(t, repo, result)
}

func TestConvertToConcreteRepo_UnknownType(t *testing.T) {
	unknown := &runtime.Unstructured{Data: map[string]any{"type": "unknown"}}
	_, err := convertToConcreteRepo(unknown)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown repository type")
}

func TestChooseAddType_OCI(t *testing.T) {
	repo := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}}
	typ, err := chooseAddType(repo)
	require.NoError(t, err)
	assert.Equal(t, ociv1alpha1.OCIAddComponentVersionV1alpha1, typ)
}

func TestChooseAddType_CTF(t *testing.T) {
	repo := &ctfv1.Repository{Type: runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version}}
	typ, err := chooseAddType(repo)
	require.NoError(t, err)
	assert.Equal(t, ociv1alpha1.CTFAddComponentVersionV1alpha1, typ)
}

func TestChooseGetLocalResourceType_OCI(t *testing.T) {
	repo := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}}
	typ, err := chooseGetLocalResourceType(repo)
	require.NoError(t, err)
	assert.Equal(t, ociv1alpha1.OCIGetLocalResourceV1alpha1, typ)
}

func TestChooseGetLocalResourceType_CTF(t *testing.T) {
	repo := &ctfv1.Repository{Type: runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version}}
	typ, err := chooseGetLocalResourceType(repo)
	require.NoError(t, err)
	assert.Equal(t, ociv1alpha1.CTFGetLocalResourceV1alpha1, typ)
}

func TestChooseAddLocalResourceType_OCI(t *testing.T) {
	repo := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}}
	typ, err := chooseAddLocalResourceType(repo)
	require.NoError(t, err)
	assert.Equal(t, ociv1alpha1.OCIAddLocalResourceV1alpha1, typ)
}

func TestChooseAddLocalResourceType_CTF(t *testing.T) {
	repo := &ctfv1.Repository{Type: runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version}}
	typ, err := chooseAddLocalResourceType(repo)
	require.NoError(t, err)
	assert.Equal(t, ociv1alpha1.CTFAddLocalResourceV1alpha1, typ)
}

func TestIsOCICompliantManifest(t *testing.T) {
	assert.True(t, isOCICompliantManifest(ocispecv1.MediaTypeImageManifest))
	assert.True(t, isOCICompliantManifest(ocispecv1.MediaTypeImageIndex))
	assert.False(t, isOCICompliantManifest("application/vnd.docker.distribution.manifest.v2+json"))
	assert.False(t, isOCICompliantManifest(""))
}

func TestAsUnstructured(t *testing.T) {
	repo := &oci.Repository{
		Type:    runtime.Type{Name: oci.Type, Version: "v1"},
		BaseUrl: "ghcr.io",
	}
	u, err := asUnstructured(repo)
	require.NoError(t, err)
	require.NotNil(t, u)
	require.NotNil(t, u.Data)
	assert.Equal(t, "ghcr.io", u.Data["baseUrl"])
}

func TestRepositoryEqual_OCISameValues(t *testing.T) {
	a := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io", SubPath: "org"}
	b := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io", SubPath: "org"}
	assert.True(t, RepositoryEqual(a, b))
}

func TestRepositoryEqual_OCIDifferentBaseUrl(t *testing.T) {
	a := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io"}
	b := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "docker.io"}
	assert.False(t, RepositoryEqual(a, b))
}

func TestRepositoryEqual_OCIDifferentSubPath(t *testing.T) {
	a := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io", SubPath: "org1"}
	b := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io", SubPath: "org2"}
	assert.False(t, RepositoryEqual(a, b))
}

func TestRepositoryEqual_CTFSameValues(t *testing.T) {
	a := &ctfv1.Repository{Type: runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version}, FilePath: "/tmp/a"}
	b := &ctfv1.Repository{Type: runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version}, FilePath: "/tmp/a"}
	assert.True(t, RepositoryEqual(a, b))
}

func TestRepositoryEqual_CTFDifferentPath(t *testing.T) {
	a := &ctfv1.Repository{Type: runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version}, FilePath: "/tmp/a"}
	b := &ctfv1.Repository{Type: runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version}, FilePath: "/tmp/b"}
	assert.False(t, RepositoryEqual(a, b))
}

func TestRepositoryEqual_DifferentTypes(t *testing.T) {
	a := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io"}
	b := &ctfv1.Repository{Type: runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version}, FilePath: "/tmp/a"}
	assert.False(t, RepositoryEqual(a, b))
}

func TestAppendUniqueRepositories_NoDuplicates(t *testing.T) {
	t1 := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io/t1"}
	t2 := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io/t2"}

	result := AppendUniqueRepositories(nil, []runtime.Typed{t1, t2})
	assert.Len(t, result, 2)

	// Same pointer
	result = AppendUniqueRepositories(result, []runtime.Typed{t1})
	assert.Len(t, result, 2)

	// Different pointer, same values
	t1Copy := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io/t1"}
	result = AppendUniqueRepositories(result, []runtime.Typed{t1Copy})
	assert.Len(t, result, 2, "value-equal repo should not be added again")
}

func TestAppendUniqueRepositories_MixedTypes(t *testing.T) {
	ociRepo := &oci.Repository{Type: runtime.Type{Name: oci.Type, Version: "v1"}, BaseUrl: "ghcr.io"}
	ctfRepo := &ctfv1.Repository{Type: runtime.Type{Name: ctfv1.Type, Version: ctfv1.Version}, FilePath: "/tmp/a"}

	result := AppendUniqueRepositories(nil, []runtime.Typed{ociRepo, ctfRepo})
	assert.Len(t, result, 2, "different types should both be kept")
}

func TestGetReferenceName(t *testing.T) {
	tests := []struct {
		name          string
		ociImage      ociv1.OCIImage
		wantReference string
		wantErr       bool
		errContains   string
	}{
		{
			name:          "valid oci reference with scheme and path",
			ociImage:      ociv1.OCIImage{ImageReference: "oci://registry.io/path/to/image:tag"},
			wantReference: "path/to/image:tag",
		},
		{
			name:          "valid https reference",
			ociImage:      ociv1.OCIImage{ImageReference: "https://registry.io/path/to/image"},
			wantReference: "path/to/image",
		},
		{
			name:          "valid http reference",
			ociImage:      ociv1.OCIImage{ImageReference: "http://registry.io/path/to/image"},
			wantReference: "path/to/image",
		},
		{
			name:          "valid path only reference",
			ociImage:      ociv1.OCIImage{ImageReference: "/path/to/image"},
			wantReference: "path/to/image",
		},
		{
			name:          "valid reference with port",
			ociImage:      ociv1.OCIImage{ImageReference: "oci://registry.io:5000/path/to/image"},
			wantReference: "path/to/image",
		},
		{
			name:          "valid reference with digest",
			ociImage:      ociv1.OCIImage{ImageReference: "oci://registry.io/image@sha256:d49cede63746a2d5a7de9f8b13937966e5bddd2bb8e36100d852f71c7e282351"},
			wantReference: "image",
		},
		{
			name:          "valid reference with tag and digest",
			ociImage:      ociv1.OCIImage{ImageReference: "oci://registry.io/image:v1.0.0@sha256:d49cede63746a2d5a7de9f8b13937966e5bddd2bb8e36100d852f71c7e282351"},
			wantReference: "image:v1.0.0",
		},
		{
			name:          "valid reference with multiple path segments",
			ociImage:      ociv1.OCIImage{ImageReference: "oci://registry.io/org/project/component/image:ociv1.0.0"},
			wantReference: "org/project/component/image:ociv1.0.0",
		},
		{name: "invalid reference with query parameters", ociImage: ociv1.OCIImage{ImageReference: "oci://registry.io/path/to/image?param=value"}, wantErr: true},
		{name: "invalid reference with fragment", ociImage: ociv1.OCIImage{ImageReference: "oci://registry.io/path/to/image#fragment"}, wantErr: true},
		{name: "invalid empty image reference", ociImage: ociv1.OCIImage{ImageReference: ""}, wantErr: true},
		{name: "invalid reference with invalid characters in scheme", ociImage: ociv1.OCIImage{ImageReference: "ht!tp://registry.io/path/to/image"}, wantErr: true, errContains: "invalid OCI image reference"},
		{name: "invalid reference with control characters", ociImage: ociv1.OCIImage{ImageReference: "oci://registry.io/path\x00/image"}, wantErr: true, errContains: "invalid OCI image reference"},
		{name: "invalid reference with backslashes", ociImage: ociv1.OCIImage{ImageReference: "oci://registry.io\\path\\to\\image"}, wantErr: true, errContains: "invalid OCI image reference"},
		{name: "invalid reference with newline", ociImage: ociv1.OCIImage{ImageReference: "oci://registry.io/path\n/image"}, wantErr: true, errContains: "invalid OCI image reference"},
		{name: "invalid reference with tab character", ociImage: ociv1.OCIImage{ImageReference: "oci://registry.io/path\t/image"}, wantErr: true, errContains: "invalid OCI image reference"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			gotReference, gotErr := getReferenceName(tt.ociImage.ImageReference)
			if tt.wantErr {
				r.Error(gotErr)
				if tt.errContains != "" {
					r.ErrorContains(gotErr, tt.errContains)
				}
			} else {
				r.NoError(gotErr)
			}
			r.Equal(tt.wantReference, gotReference)
		})
	}
}
