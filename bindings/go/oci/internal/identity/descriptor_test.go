package identity

import (
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

func TestAdoptAsResource(t *testing.T) {
	tests := []struct {
		name          string
		desc          *ociImageSpecV1.Descriptor
		resource      *descriptor.Resource
		expectedError string
		validate      func(t *testing.T, desc *ociImageSpecV1.Descriptor)
	}{
		{
			name: "success with all platform attributes",
			desc: &ociImageSpecV1.Descriptor{},
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ExtraIdentity: map[string]string{
						"architecture": "amd64",
						"os":           "linux",
						"variant":      "v1",
						"os.features":  "feature1,feature2",
						"os.version":   "1.0.0",
					},
				},
			},
			validate: func(t *testing.T, desc *ociImageSpecV1.Descriptor) {
				assert.NotNil(t, desc.Platform)
				assert.Equal(t, "amd64", desc.Platform.Architecture)
				assert.Equal(t, "linux", desc.Platform.OS)
				assert.Equal(t, "v1", desc.Platform.Variant)
				assert.Equal(t, []string{"feature1", "feature2"}, desc.Platform.OSFeatures)
				assert.Equal(t, "1.0.0", desc.Platform.OSVersion)
				assert.NotEmpty(t, desc.Annotations)
			},
		},
		{
			name: "success with partial platform attributes",
			desc: &ociImageSpecV1.Descriptor{},
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ExtraIdentity: map[string]string{
						"architecture": "arm64",
						"os":           "darwin",
					},
				},
			},
			validate: func(t *testing.T, desc *ociImageSpecV1.Descriptor) {
				assert.NotNil(t, desc.Platform)
				assert.Equal(t, "arm64", desc.Platform.Architecture)
				assert.Equal(t, "darwin", desc.Platform.OS)
				assert.Empty(t, desc.Platform.Variant)
				assert.Empty(t, desc.Platform.OSFeatures)
				assert.Empty(t, desc.Platform.OSVersion)
				assert.NotEmpty(t, desc.Annotations)
			},
		},
		{
			name: "success with no platform attributes",
			desc: &ociImageSpecV1.Descriptor{},
			resource: &descriptor.Resource{
				ElementMeta: descriptor.ElementMeta{
					ExtraIdentity: map[string]string{},
				},
			},
			validate: func(t *testing.T, desc *ociImageSpecV1.Descriptor) {
				assert.Nil(t, desc.Platform)
				assert.NotEmpty(t, desc.Annotations)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AdoptAsResource(tt.desc, tt.resource)

			if tt.expectedError != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
				return
			}

			require.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t, tt.desc)
			}
		})
	}
}
