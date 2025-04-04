package oci

import (
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestLayerFromResourceIdentityAndLocalBlob(t *testing.T) {
	scheme := runtime.NewScheme()
	v2.MustAddToScheme(scheme)

	// Create a valid digest for testing
	testDigest := digest.FromString("test").String()

	tests := []struct {
		name     string
		access   *v2.LocalBlob
		size     int64
		resource *descriptor.Resource
		want     func(t *testing.T, got ociImageSpecV1.Descriptor)
	}{
		{
			name: "basic resource without platform",
			access: &v2.LocalBlob{
				Type: runtime.Type{
					Name:    v2.LocalBlobAccessType,
					Version: v2.LocalBlobAccessTypeVersion,
				},
				MediaType:      "application/octet-stream",
				LocalReference: testDigest,
			},
			size: 100,
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
				},
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobAccessType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
					MediaType:      "application/octet-stream",
					LocalReference: testDigest,
				},
			},
			want: func(t *testing.T, got ociImageSpecV1.Descriptor) {
				assert.Equal(t, "application/octet-stream", got.MediaType)
				assert.Equal(t, digest.Digest(testDigest), got.Digest)
				assert.Equal(t, int64(100), got.Size)
				assert.Nil(t, got.Platform)
			},
		},
		{
			name: "resource with platform attributes",
			access: &v2.LocalBlob{
				Type: runtime.Type{
					Name:    v2.LocalBlobAccessType,
					Version: v2.LocalBlobAccessTypeVersion,
				},
				MediaType:      "application/octet-stream",
				LocalReference: testDigest,
			},
			size: 100,
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ObjectMeta: descriptor.ObjectMeta{
						Name:    "test",
						Version: "1.0.0",
					},
					ExtraIdentity: map[string]string{
						"architecture": "amd64",
						"os":           "linux",
						"variant":      "v1",
						"os.features":  "feature1,feature2",
						"os.version":   "1.0",
					},
				},
				Access: &v2.LocalBlob{
					Type: runtime.Type{
						Name:    v2.LocalBlobAccessType,
						Version: v2.LocalBlobAccessTypeVersion,
					},
					MediaType:      "application/octet-stream",
					LocalReference: testDigest,
				},
			},
			want: func(t *testing.T, got ociImageSpecV1.Descriptor) {
				assert.Equal(t, "application/octet-stream", got.MediaType)
				assert.Equal(t, digest.Digest(testDigest), got.Digest)
				assert.Equal(t, int64(100), got.Size)
				require.NotNil(t, got.Platform)
				assert.Equal(t, "amd64", got.Platform.Architecture)
				assert.Equal(t, "linux", got.Platform.OS)
				assert.Equal(t, "v1", got.Platform.Variant)
				assert.Equal(t, []string{"feature1", "feature2"}, got.Platform.OSFeatures)
				assert.Equal(t, "1.0", got.Platform.OSVersion)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newLocalResourceLayer(scheme, tt.size, tt.resource)
			require.NoError(t, err)
			tt.want(t, got)
		})
	}
}
