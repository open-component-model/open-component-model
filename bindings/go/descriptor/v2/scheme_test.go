package v2_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme_ResolvesAllLocalBlobAliases(t *testing.T) {
	tests := []struct {
		name string
		typ  runtime.Type
	}{
		{"versioned", runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion)},
		{"unversioned", runtime.NewUnversionedType(descriptorv2.LocalBlobAccessType)},
		{"legacy versioned", runtime.NewVersionedType(descriptorv2.LegacyLocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion)},
		{"legacy unversioned", runtime.NewUnversionedType(descriptorv2.LegacyLocalBlobAccessType)},
		{"legacy oci blob versioned", runtime.NewVersionedType(descriptorv2.LegacyLocalOciBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion)},
		{"legacy oci blob unversioned", runtime.NewUnversionedType(descriptorv2.LegacyLocalOciBlobAccessType)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := descriptorv2.Scheme.NewObject(tt.typ)
			require.NoError(t, err)
			require.IsType(t, &descriptorv2.LocalBlob{}, obj)
		})
	}
}

// TestScheme_ConvertsLegacyLocalOciBlobAccess covers the conversion the repositories and the
// transfer perform on a stored access (see oci.Repository.GetLocalResource and
// transfer's scanLocalBlobs): an OCM v1 localOciBlob must arrive as a LocalBlob whose
// LocalReference resolves the blob.
func TestScheme_ConvertsLegacyLocalOciBlobAccess(t *testing.T) {
	r := require.New(t)

	raw := &runtime.Raw{
		Type: runtime.NewVersionedType(descriptorv2.LegacyLocalOciBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
		Data: []byte(`{"type":"localOciBlob/v1","digest":"sha256:abc123"}`),
	}

	var local descriptorv2.LocalBlob
	r.NoError(descriptorv2.Scheme.Convert(raw, &local))
	r.Equal("sha256:abc123", local.LocalReference)
	r.Empty(local.MediaType)
	r.True(descriptorv2.IsLocalBlob(raw))
}
