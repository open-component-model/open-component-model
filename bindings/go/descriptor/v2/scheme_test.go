package v2_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme_ResolvesUpperCamelCase_LocalBlob(t *testing.T) {
	obj, err := descriptorv2.Scheme.NewObject(
		runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
	)
	require.NoError(t, err, "Scheme must resolve UpperCamelCase type LocalBlob/v1")
	require.IsType(t, &descriptorv2.LocalBlob{}, obj, "expected *LocalBlob from Scheme")
}

func TestScheme_ResolvesLegacy_LocalBlob(t *testing.T) {
	obj, err := descriptorv2.Scheme.NewObject(
		runtime.NewVersionedType(descriptorv2.LegacyLocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion),
	)
	require.NoError(t, err, "Scheme must resolve legacy type localBlob/v1")
	require.IsType(t, &descriptorv2.LocalBlob{}, obj, "expected *LocalBlob from Scheme")
}

func TestScheme_ResolvesUnversioned_LocalBlob(t *testing.T) {
	obj, err := descriptorv2.Scheme.NewObject(
		runtime.NewUnversionedType(descriptorv2.LocalBlobAccessType),
	)
	require.NoError(t, err, "Scheme must resolve unversioned LocalBlob")
	require.IsType(t, &descriptorv2.LocalBlob{}, obj, "expected *LocalBlob from Scheme")
}

func TestScheme_ResolvesUnversionedLegacy_LocalBlob(t *testing.T) {
	obj, err := descriptorv2.Scheme.NewObject(
		runtime.NewUnversionedType(descriptorv2.LegacyLocalBlobAccessType),
	)
	require.NoError(t, err, "Scheme must resolve unversioned legacy localBlob")
	require.IsType(t, &descriptorv2.LocalBlob{}, obj, "expected *LocalBlob from Scheme")
}
