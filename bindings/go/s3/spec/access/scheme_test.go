package access_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	"ocm.software/open-component-model/bindings/go/s3/spec/access/v2"
)

// The spellings registered here are the ones the JSON schema declares for the type
// field, so a descriptor that validates against the schema also converts. Lookups are
// exact, which is why the alias cannot be derived from [v2.Type] by lower-casing it.
func TestScheme_ResolvesAllS3Aliases(t *testing.T) {
	tests := []struct {
		name string
		typ  runtime.Type
	}{
		{"S3 versioned", runtime.NewVersionedType(v2.Type, v2.Version)},
		{"S3 unversioned", runtime.NewUnversionedType(v2.Type)},
		{"s3 versioned", runtime.NewVersionedType(v2.LowerCamelType, v2.Version)},
		{"s3 unversioned", runtime.NewUnversionedType(v2.LowerCamelType)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj, err := accessspec.Scheme.NewObject(tt.typ)
			require.NoError(t, err)
			require.IsType(t, &v2.S3{}, obj)
		})
	}
}

// The package documentation states that no spelling other than the four registered
// ones resolves.
func TestScheme_RejectsUndeclaredSpellings(t *testing.T) {
	for _, name := range []string{"S3/v1", "s3/v1", "S3Bucket/v1", "S3Bucket", "s3bucket"} {
		t.Run(name, func(t *testing.T) {
			typ, err := runtime.TypeFromString(name)
			require.NoError(t, err)

			_, err = accessspec.Scheme.NewObject(typ)
			require.Error(t, err, "only the spellings the JSON schema declares may resolve")
		})
	}
}

// An access spec ocmv1 wrote in its v2 format resolves unchanged.
func TestScheme_ReadsOCMv1V2Format(t *testing.T) {
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal([]byte(`{"type":"s3/v2","region":"r","bucketName":"b","objectKey":"k","version":"x","mediaType":"m"}`), raw))

	spec := &v2.S3{}
	require.NoError(t, accessspec.Scheme.Convert(raw, spec))
	require.Equal(t, v2.S3{
		Type:       runtime.NewVersionedType(v2.LowerCamelType, v2.Version),
		Region:     "r",
		BucketName: "b",
		ObjectKey:  "k",
		Version:    "x",
		MediaType:  "m",
	}, *spec)
}

// ocmv1 writes an unversioned "s3" in its v1 format (bucket, key) by default. That shape
// is read as v2 and must fail validation rather than address an empty bucket.
func TestScheme_OCMv1V1FormatFailsValidation(t *testing.T) {
	raw := &runtime.Raw{}
	require.NoError(t, json.Unmarshal([]byte(`{"type":"s3","bucket":"b","key":"k"}`), raw))

	spec := &v2.S3{}
	require.NoError(t, accessspec.Scheme.Convert(raw, spec))
	require.ErrorContains(t, spec.Validate(), "bucketName is required")
}
