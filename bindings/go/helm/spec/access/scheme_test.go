package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	access "ocm.software/open-component-model/bindings/go/helm/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/helm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme_ResolvesAllHelmAccessAliases(t *testing.T) {
	tests := []struct {
		name string
		typ  runtime.Type
	}{
		{"Helm versioned", runtime.NewVersionedType(v1.Type, v1.Version)},
		{"Helm unversioned", runtime.NewUnversionedType(v1.Type)},
		{"helm legacy versioned", runtime.NewVersionedType(v1.LegacyType, v1.LegacyTypeVersion)},
		{"helm legacy unversioned", runtime.NewUnversionedType(v1.LegacyType)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := access.Scheme.NewObject(tt.typ)
			require.NoError(t, err)
			require.IsType(t, &v1.Helm{}, obj)
		})
	}
}
