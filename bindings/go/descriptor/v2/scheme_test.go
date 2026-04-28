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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := descriptorv2.Scheme.NewObject(tt.typ)
			require.NoError(t, err)
			require.IsType(t, &descriptorv2.LocalBlob{}, obj)
		})
	}
}
