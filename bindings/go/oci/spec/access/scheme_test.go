package oci_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	oci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme_ResolvesAllOCIImageAliases(t *testing.T) {
	tests := []struct {
		name string
		typ  runtime.Type
	}{
		{"OCIImage versioned", runtime.NewVersionedType(v1.OCIImageType, v1.Version)},
		{"OCIImage unversioned", runtime.NewUnversionedType(v1.OCIImageType)},
		{"ociArtifact versioned", runtime.NewVersionedType(v1.LegacyType, v1.LegacyTypeVersion)},
		{"ociArtifact unversioned", runtime.NewUnversionedType(v1.LegacyType)},
		{"ociRegistry versioned", runtime.NewVersionedType(v1.LegacyType2, v1.LegacyType2Version)},
		{"ociRegistry unversioned", runtime.NewUnversionedType(v1.LegacyType2)},
		{"ociImage versioned", runtime.NewVersionedType(v1.LegacyType3, v1.LegacyType3Version)},
		{"ociImage unversioned", runtime.NewUnversionedType(v1.LegacyType3)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := oci.Scheme.NewObject(tt.typ)
			require.NoError(t, err)
			require.IsType(t, &v1.OCIImage{}, obj)
		})
	}
}

func TestScheme_ResolvesAllOCIImageLayerAliases(t *testing.T) {
	tests := []struct {
		name string
		typ  runtime.Type
	}{
		{"OCIImageLayer versioned", runtime.NewVersionedType(v1.OCIImageLayerType, v1.Version)},
		{"OCIImageLayer unversioned", runtime.NewUnversionedType(v1.OCIImageLayerType)},
		{"ociBlob versioned", runtime.NewVersionedType(v1.LegacyOCIBlobAccessType, v1.LegacyOCIBlobAccessTypeVersion)},
		{"ociBlob unversioned", runtime.NewUnversionedType(v1.LegacyOCIBlobAccessType)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := oci.Scheme.NewObject(tt.typ)
			require.NoError(t, err)
			require.IsType(t, &v1.OCIImageLayer{}, obj)
		})
	}
}
