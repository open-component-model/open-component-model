package v2_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme_ResolvesLocalBlob(t *testing.T) {
	cases := []struct {
		name string
		typ  runtime.Type
	}{
		{"UpperCamelCase versioned", runtime.NewVersionedType(descriptorv2.LocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion)},
		{"legacy versioned", runtime.NewVersionedType(descriptorv2.LegacyLocalBlobAccessType, descriptorv2.LocalBlobAccessTypeVersion)},
		{"unversioned", runtime.NewUnversionedType(descriptorv2.LocalBlobAccessType)},
		{"unversioned legacy", runtime.NewUnversionedType(descriptorv2.LegacyLocalBlobAccessType)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj, err := descriptorv2.Scheme.NewObject(tc.typ)
			require.NoError(t, err)
			require.IsType(t, &descriptorv2.LocalBlob{}, obj)
		})
	}
}
