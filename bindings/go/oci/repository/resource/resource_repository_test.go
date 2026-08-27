package resource

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	filesystemv1alpha1 "ocm.software/open-component-model/bindings/go/configuration/filesystem/v1alpha1/spec"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
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

func TestAddOwnership_RawAccessType(t *testing.T) {
	// As with ProcessResourceDigest, a resource coming from a component descriptor
	// carries *runtime.Raw access. AddOwnership must convert it to the typed
	// spec before handing off to the inner repository's AddOwnership, which then
	// dispatches on the typed access to the by-reference path.
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
	err := repo.AddOwnership(t.Context(), "ocm.software/test", "1.0.0", res, nil)

	// Conversion succeeded only if execution reached the inner AddOwnership by-reference
	// path and failed resolving the unreachable host. Assert that positively so a reworded
	// conversion error can't make this pass for the wrong reason.
	require.ErrorContains(t, err, "nonexistent.invalid",
		"AddOwnership must convert *runtime.Raw access to typed and reach the inner repository")
}

// rawOCIImageLayer builds the *runtime.Raw access a resource carries when read
// back from a component descriptor.
func rawOCIImageLayer(t *testing.T, reference string) *runtime.Raw {
	t.Helper()
	raw := &runtime.Raw{}
	require.NoError(t, ociaccess.Scheme.Convert(&v1.OCIImageLayer{
		Type:      runtime.NewVersionedType(v1.OCIImageLayerType, v1.Version),
		Reference: reference,
		MediaType: "application/octet-stream",
		Digest:    digest.FromString("layer"),
		Size:      5,
	}, raw))
	return raw
}

func layerResource(t *testing.T, reference string) *descriptor.Resource {
	t.Helper()
	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{
			ObjectMeta: descriptor.ObjectMeta{Name: "test", Version: "1.0.0"},
		},
		Type:   "helmChart",
		Access: rawOCIImageLayer(t, reference),
	}
}

// The repository registers OCIImageLayer in its scheme, so the plugin manager routes
// such resources here and every read path has to accept the type.
func TestOCIImageLayer_ReadPathsAccepted(t *testing.T) {
	ref := "nonexistent.invalid/test@" + digest.FromString("layer").String()

	t.Run("credential consumer identity", func(t *testing.T) {
		repo := NewResourceRepository(nil)
		identity, err := repo.GetResourceCredentialConsumerIdentity(t.Context(), layerResource(t, ref))
		require.NoError(t, err)
		require.Equal(t, "nonexistent.invalid", identity["hostname"])
	})

	t.Run("digest processor identity", func(t *testing.T) {
		repo := NewResourceRepository(nil)
		identity, err := repo.GetResourceDigestProcessorCredentialConsumerIdentity(t.Context(), layerResource(t, ref))
		require.NoError(t, err)
		require.Equal(t, "nonexistent.invalid", identity["hostname"])
	})

	t.Run("digest processing reaches the registry", func(t *testing.T) {
		repo := NewResourceRepository(nil)
		_, err := repo.ProcessResourceDigest(t.Context(), layerResource(t, ref), nil)
		// Asserting on the host, not on the absence of a rejection: reaching DNS is
		// what proves the access type was accepted.
		require.ErrorContains(t, err, "nonexistent.invalid")
	})

	t.Run("download reaches the registry", func(t *testing.T) {
		repo := NewResourceRepository(nil)
		_, err := repo.DownloadResource(t.Context(), layerResource(t, ref), nil)
		require.ErrorContains(t, err, "nonexistent.invalid")
	})
}

// A layer addresses one existing blob and cannot be an upload destination, so the
// upload paths reject it up front rather than deep inside the inner repository.
func TestOCIImageLayer_RejectedAsUploadTarget(t *testing.T) {
	ref := "nonexistent.invalid/test@" + digest.FromString("layer").String()

	repo := NewResourceRepository(nil)
	_, err := repo.UploadResource(t.Context(), layerResource(t, ref), nil, nil)
	require.ErrorContains(t, err, "as upload target")

	_, err = repo.UploadResourceStream(t.Context(), layerResource(t, ref), nil, nil)
	require.ErrorContains(t, err, "as upload target")
}

// An access that spells the reference field wrong deserializes into an empty one,
// which has to be reported against the field the type actually uses.
func TestOCIImageLayer_MissingReferenceNamesField(t *testing.T) {
	repo := NewResourceRepository(nil)
	_, err := repo.ProcessResourceDigest(t.Context(), layerResource(t, ""), nil)
	require.ErrorContains(t, err, `set it in field "ref"`)
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

			repo, err := createRepository(spec, &credentials, tt.filesystemConfig, "test", http.DefaultClient)

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

func TestNewResourceRepositoryHTTPConfig_InsecureSkipVerify(t *testing.T) {
	var serverHit bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverHit = true
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	host := strings.TrimPrefix(server.URL, "https://")
	tr := true

	tests := []struct {
		name                 string
		httpConfig           *httpv1alpha1.Config
		expectHit            bool
		expectErrMsgContains string
	}{
		{
			name:                 "without config the self-signed cert is rejected",
			expectErrMsgContains: "certificate",
		},
		{
			name: "per-host insecureSkipVerify reaches the registry",
			httpConfig: &httpv1alpha1.Config{
				Hosts: map[string]*httpv1alpha1.HostConfig{
					host: {
						TLSConfig: httpv1alpha1.TLSConfig{
							InsecureSkipVerify: &tr,
						},
					},
				},
			},
			expectHit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverHit = false

			raw := &runtime.Raw{}
			require.NoError(t, ociaccess.Scheme.Convert(
				&v1.OCIImage{
					Type:           runtime.NewVersionedType(v1.OCIImageType, v1.Version),
					ImageReference: "https://" + host + "/test/img:v1.0.0",
				},
				raw,
			))

			res := &descriptor.Resource{
				Type:   "ociArtifact",
				Access: raw,
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
			}

			repo := NewResourceRepository(nil, WithHTTPConfig(tt.httpConfig))
			_, err := repo.DownloadResource(t.Context(), res, nil)
			if tt.expectErrMsgContains != "" {
				require.ErrorContains(t, err, tt.expectErrMsgContains)
			}
			require.Equal(t, tt.expectHit, serverHit, "expected HTTP request to reach test server")
		})
	}
}
