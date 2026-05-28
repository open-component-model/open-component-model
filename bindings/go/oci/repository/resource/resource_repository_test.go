package resource

import (
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	ocicredsv1 "ocm.software/open-component-model/bindings/go/oci/spec/credentials/v1"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestProcessResourceDigest_RawAccessType(t *testing.T) {
	// v2.Resource.Access is always *runtime.Raw when deserialized from a component
	// descriptor, so this path is exercised on every real resource coming from an OCI
	// registry.
	raw := &runtime.Raw{}
	require.NoError(t, ociaccess.Scheme.Convert(&v1.OCIImage{
		Type:           runtime.NewVersionedType(v1.OCIImageType, v1.Version),
		ImageReference: "nonexistent.invalid/test:v1.0.0",
	}, raw))

	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "test", Version: "1.0.0"},
		},
		Type:   "ociArtifact",
		Access: raw,
	}

	repo := NewResourceRepository(nil)
	_, err := repo.ProcessResourceDigest(t.Context(), res, nil)

	// Without the fix: error is "unsupported resource access type: *runtime.Raw"
	// With the fix:    error is a network/DNS failure reaching nonexistent.invalid
	require.Error(t, err)
	require.NotContains(t, err.Error(), "unsupported resource access type",
		"ProcessResourceDigest must convert *runtime.Raw access to typed before passing to the inner repository")
}

func TestAddOwnershipReferrer_RawAccessType(t *testing.T) {
	// As with ProcessResourceDigest, a resource coming from a component descriptor
	// carries *runtime.Raw access. AddOwnershipReferrer must convert it to the typed
	// spec before handing off to the inner repository's AddOwnershipReferrer, whose
	// type-switch only matches the typed access.
	raw := &runtime.Raw{}
	require.NoError(t, ociaccess.Scheme.Convert(&v1.OCIImage{
		Type:           runtime.NewVersionedType(v1.OCIImageType, v1.Version),
		ImageReference: "nonexistent.invalid/test:v1.0.0",
	}, raw))

	res := &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "test", Version: "1.0.0"},
		},
		Type:   "ociArtifact",
		Access: raw,
	}

	repo := NewResourceRepository(nil)
	err := repo.AddOwnershipReferrer(t.Context(), "ocm.software/test", "1.0.0", res, nil)

	// Conversion succeeded only if execution reached the inner AddOwnershipReferrer
	// and failed resolving the unreachable host. Assert that positively so a reworded
	// conversion error can't make this pass for the wrong reason.
	require.ErrorContains(t, err, "nonexistent.invalid",
		"AddOwnershipReferrer must convert *runtime.Raw access to typed and reach the inner repository")
}

func TestCreateRepositoryWithFilesystemConfig(t *testing.T) {
	r := require.New(t)

	tests := []struct {
		name             string
		filesystemConfig *filesystemv1alpha1.Config
		expectError      bool
	}{
		{
			name: "with filesystem config",
			filesystemConfig: &filesystemv1alpha1.Config{
				TempFolder: "/tmp/test",
			},
			expectError: false,
		},
		{
			name:        "without filesystem config",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &ociv1.Repository{
				BaseUrl: "localhost:5000",
			}
			credentials := ocicredsv1.OCICredentials{}

			repo, err := createRepository(spec, &credentials, tt.filesystemConfig, "test")

			if tt.expectError {
				r.Error(err, "expected error")
				r.Nil(repo, "repository should be nil")
			} else {
				r.NoError(err, "should not error")
				r.NotNil(repo, "repository should not be nil")
			}
		})
	}
}
