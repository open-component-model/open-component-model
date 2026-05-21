package resource

import (
	"testing"

	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	ociaccess "ocm.software/open-component-model/bindings/go/oci/spec/access"
	ociaccessv1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
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

// TestResourceRepository_LookupResourceOwners covers the error branches of
// the OCI wrapper that the lower-level [LookupOwners] test (in
// ownership_test.go) and the integration test do not isolate. The happy path
// hits the Referrers API and lives with the integration test.
func TestResourceRepository_LookupResourceOwners(t *testing.T) {
	// In production the registry's GetResourcePlugin defaults the Access's
	// Type via scheme.DefaultType before dispatching, so the wrapper is
	// always called with a Type-set access. These tests bypass the registry
	// and call the wrapper directly, so the Type field has to be set
	// explicitly when the test wants to reach past the first guard.
	ociType := runtime.NewVersionedType(ociaccessv1.OCIImageType, ociaccessv1.Version)

	tests := []struct {
		name        string
		access      runtime.Typed
		credentials runtime.Typed
		errContains string
	}{
		{
			name:        "unknown access type fails at scheme.NewObject",
			access:      &runtime.Raw{Type: runtime.NewVersionedType("unknown.example", "v1")},
			errContains: "creating new object",
		},
		{
			name:        "malformed image reference fails at looseref.ParseReference",
			access:      &ociaccessv1.OCIImage{Type: ociType, ImageReference: "://bad"},
			errContains: "parsing image reference",
		},
		{
			name:        "unsupported credential type fails at ConvertToOCICredentials",
			access:      &ociaccessv1.OCIImage{Type: ociType, ImageReference: "ghcr.io/acme/image:v1"},
			credentials: &runtime.Raw{Type: runtime.NewVersionedType("unknown.credentials", "v1")},
			errContains: "converting credentials",
		},
	}

	repo := NewResourceRepository(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &descriptor.Resource{Access: tt.access}
			_, err := repo.LookupResourceOwners(t.Context(), res, tt.credentials)
			require.ErrorContains(t, err, tt.errContains)
		})
	}
}
