package access_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

// The spellings registered here are the ones the JSON schema declares for the type
// field, so a descriptor that validates against the schema also converts. Lookups are
// exact, which is why the alias cannot be derived from [v1.Type] by lower-casing it.
func TestScheme_ResolvesAllS3BucketAliases(t *testing.T) {
	tests := []struct {
		name string
		typ  runtime.Type
	}{
		{"S3Bucket versioned", runtime.NewVersionedType(v1.Type, v1.Version)},
		{"S3Bucket unversioned", runtime.NewUnversionedType(v1.Type)},
		{"s3Bucket versioned", runtime.NewVersionedType(v1.LowerCamelType, v1.Version)},
		{"s3Bucket unversioned", runtime.NewUnversionedType(v1.LowerCamelType)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := accessspec.Scheme.NewObject(tt.typ)
			require.NoError(t, err)
			require.IsType(t, &v1.S3Bucket{}, obj)
		})
	}
}

// The package documentation states that no spelling other than the four registered
// ones resolves.
func TestScheme_RejectsUndeclaredSpellings(t *testing.T) {
	for _, name := range []string{"s3bucket/v1", "s3bucket", "S3BUCKET/v1", "s3", "s3/v1"} {
		t.Run(name, func(t *testing.T) {
			typ, err := runtime.TypeFromString(name)
			require.NoError(t, err)

			_, err = accessspec.Scheme.NewObject(typ)
			require.Error(t, err, "only the spellings the JSON schema declares may resolve")
		})
	}
}
