package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	access "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access"
	v1alpha1 "ocm.software/open-component-model/bindings/go/blob/filesystem/spec/access/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestScheme_ResolvesAllFileAccessAliases(t *testing.T) {
	tests := []struct {
		name string
		typ  runtime.Type
	}{
		{"File versioned", runtime.NewVersionedType(v1alpha1.FileType, v1alpha1.Version)},
		{"File unversioned", runtime.NewUnversionedType(v1alpha1.FileType)},
		{"file legacy versioned", runtime.NewVersionedType(v1alpha1.LegacyFileType, v1alpha1.Version)},
		{"file legacy unversioned", runtime.NewUnversionedType(v1alpha1.LegacyFileType)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := access.Scheme.NewObject(tt.typ)
			require.NoError(t, err)
			require.IsType(t, &v1alpha1.File{}, obj)
		})
	}
}
