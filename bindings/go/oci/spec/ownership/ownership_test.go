package ownership_test

import (
	"errors"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/spec/annotations"
	"ocm.software/open-component-model/bindings/go/oci/spec/ownership"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestParse(t *testing.T) {
	const (
		component = "ocm.software/my-component"
		version   = "v1.0.0"
		// JCS-canonical encoding produced by the writer (internal/pack/ownership_referrer.go).
		artifactJSON = `{"identity":{"architecture":"amd64","name":"my-resource","os":"linux","version":"v1.0.0"},"kind":"resource"}`
	)

	wantArtifact := annotations.ArtifactOCIAnnotation{
		Identity: runtime.Identity{
			"architecture": "amd64",
			"name":         "my-resource",
			"os":           "linux",
			"version":      "v1.0.0",
		},
		Kind: annotations.ArtifactKindResource,
	}

	t.Run("happy path returns parsed ownership", func(t *testing.T) {
		desc := ociImageSpecV1.Descriptor{
			Annotations: map[string]string{
				annotations.OwnershipComponentName:    component,
				annotations.OwnershipComponentVersion: version,
				annotations.ArtifactAnnotationKey:     artifactJSON,
			},
		}
		got, err := ownership.Parse(desc)
		require.NoError(t, err)
		assert.Equal(t, ownership.Ownership{
			ComponentName:    component,
			ComponentVersion: version,
			Artifact:         wantArtifact,
		}, got)
	})

	t.Run("ignores unrelated annotations", func(t *testing.T) {
		desc := ociImageSpecV1.Descriptor{
			Annotations: map[string]string{
				annotations.OwnershipComponentName:    component,
				annotations.OwnershipComponentVersion: version,
				annotations.ArtifactAnnotationKey:     artifactJSON,
				annotations.OCMCreator:                "ocmcli/v9.9",
				annotations.OCMComponentVersion:       annotations.NewComponentVersionAnnotation(component, version),
			},
		}
		got, err := ownership.Parse(desc)
		require.NoError(t, err)
		assert.Equal(t, component, got.ComponentName)
		assert.Equal(t, version, got.ComponentVersion)
		assert.Equal(t, wantArtifact, got.Artifact)
	})

	missingCases := []struct {
		name        string
		annotations map[string]string
	}{
		{
			name:        "nil annotations",
			annotations: nil,
		},
		{
			name:        "empty annotations",
			annotations: map[string]string{},
		},
		{
			name: "missing component name",
			annotations: map[string]string{
				annotations.OwnershipComponentVersion: version,
				annotations.ArtifactAnnotationKey:     artifactJSON,
			},
		},
		{
			name: "missing component version",
			annotations: map[string]string{
				annotations.OwnershipComponentName: component,
				annotations.ArtifactAnnotationKey:  artifactJSON,
			},
		},
		{
			name: "missing artifact annotation",
			annotations: map[string]string{
				annotations.OwnershipComponentName:    component,
				annotations.OwnershipComponentVersion: version,
			},
		},
	}
	for _, tc := range missingCases {
		t.Run("missing keys yield ErrNotAnOwnershipReferrer/"+tc.name, func(t *testing.T) {
			_, err := ownership.Parse(ociImageSpecV1.Descriptor{Annotations: tc.annotations})
			assert.ErrorIs(t, err, ownership.ErrNotAnOwnershipReferrer)
		})
	}

	t.Run("malformed artifact annotation returns wrapping error, not sentinel", func(t *testing.T) {
		desc := ociImageSpecV1.Descriptor{
			Annotations: map[string]string{
				annotations.OwnershipComponentName:    component,
				annotations.OwnershipComponentVersion: version,
				annotations.ArtifactAnnotationKey:     `not-json`,
			},
		}
		_, err := ownership.Parse(desc)
		require.Error(t, err)
		assert.False(t, errors.Is(err, ownership.ErrNotAnOwnershipReferrer),
			"malformed payload must surface as a parse error, not a missing-annotation sentinel")
		assert.Contains(t, err.Error(), annotations.ArtifactAnnotationKey)
	})
}
