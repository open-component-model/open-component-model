package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestMustRegisterIdentityType(t *testing.T) {
	scheme := runtime.NewScheme()
	MustRegisterIdentityType(scheme)

	assert.True(t, scheme.IsRegistered(VersionedType))
	assert.True(t, scheme.IsRegistered(Type))

	obj, err := scheme.NewObject(Type)
	require.NoError(t, err)
	_, ok := obj.(*S3BucketIdentity)
	assert.True(t, ok, "expected *S3BucketIdentity, got %T", obj)
}

func TestS3BucketIdentity_SchemeConvert(t *testing.T) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	MustRegisterIdentityType(scheme)

	original := &S3BucketIdentity{
		Type:     VersionedType,
		Hostname: "s3.example.com",
		Scheme:   "https",
		Port:     "443",
		Path:     "my-bucket/path/to/object.tar.gz",
	}

	raw := &runtime.Raw{}
	require.NoError(t, scheme.Convert(original, raw))

	restored := &S3BucketIdentity{}
	require.NoError(t, scheme.Convert(raw, restored))

	assert.Equal(t, original.Type, restored.Type)
	assert.Equal(t, original.Hostname, restored.Hostname)
	assert.Equal(t, original.Scheme, restored.Scheme)
	assert.Equal(t, original.Port, restored.Port)
	assert.Equal(t, original.Path, restored.Path)
}

func TestToIdentity_NilInput(t *testing.T) {
	assert.Nil(t, ToIdentity(nil))
}

func TestFromIdentity_NilInput(t *testing.T) {
	assert.Nil(t, FromIdentity(nil))
}

func TestToIdentity(t *testing.T) {
	tests := []struct {
		name  string
		input *S3BucketIdentity
		want  runtime.Identity
	}{
		{
			name: "full identity",
			input: &S3BucketIdentity{
				Type:     VersionedType,
				Hostname: "s3.example.com",
				Scheme:   "https",
				Port:     "443",
				Path:     "my-bucket/path/to/object.tar.gz",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "s3.example.com",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributePath:     "my-bucket/path/to/object.tar.gz",
			},
		},
		{
			name: "only hostname",
			input: &S3BucketIdentity{
				Type:     VersionedType,
				Hostname: "s3.example.com",
			},
			want: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "s3.example.com",
			},
		},
		{
			name:  "empty identity uses defaulted type",
			input: &S3BucketIdentity{},
			want: runtime.Identity{
				runtime.IdentityAttributeType: VersionedType.String(),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ToIdentity(tt.input))
		})
	}
}

func TestFromIdentity(t *testing.T) {
	tests := []struct {
		name  string
		input runtime.Identity
		want  *S3BucketIdentity
	}{
		{
			name: "full identity",
			input: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "s3.example.com",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePort:     "443",
				runtime.IdentityAttributePath:     "my-bucket/path/to/object.tar.gz",
			},
			want: &S3BucketIdentity{
				Type:     VersionedType,
				Hostname: "s3.example.com",
				Scheme:   "https",
				Port:     "443",
				Path:     "my-bucket/path/to/object.tar.gz",
			},
		},
		{
			name: "only hostname",
			input: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "s3.example.com",
			},
			want: &S3BucketIdentity{
				Type:     VersionedType,
				Hostname: "s3.example.com",
			},
		},
		{
			name: "unknown attributes are ignored",
			input: runtime.Identity{
				runtime.IdentityAttributeType:     VersionedType.String(),
				runtime.IdentityAttributeHostname: "s3.example.com",
				"unrelated":                       "value",
			},
			want: &S3BucketIdentity{
				Type:     VersionedType,
				Hostname: "s3.example.com",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FromIdentity(tt.input))
		})
	}
}

// TestIdentity_RoundTrip verifies that ToIdentity followed by FromIdentity
// returns an equivalent struct (and vice versa).
func TestIdentity_RoundTrip(t *testing.T) {
	t.Run("struct -> identity -> struct", func(t *testing.T) {
		original := &S3BucketIdentity{
			Type:     VersionedType,
			Hostname: "s3.example.com",
			Scheme:   "https",
			Port:     "443",
			Path:     "my-bucket/path/to/object.tar.gz",
		}
		assert.Equal(t, original, FromIdentity(ToIdentity(original)))
	})

	t.Run("identity -> struct -> identity", func(t *testing.T) {
		original := runtime.Identity{
			runtime.IdentityAttributeType:     VersionedType.String(),
			runtime.IdentityAttributeHostname: "s3.example.com",
			runtime.IdentityAttributeScheme:   "https",
			runtime.IdentityAttributePort:     "443",
			runtime.IdentityAttributePath:     "my-bucket/path/to/object.tar.gz",
		}
		assert.Equal(t, original, ToIdentity(FromIdentity(original)))
	})
}

// TestIdentityFromObject pins the consumer identity resolved for an access spec.
// Without an endpoint only the object path is carried, because AWS is reached at a
// well-known host; a custom endpoint adds its URL attributes so an entry can be
// scoped to that store.
func TestIdentityFromObject(t *testing.T) {
	tests := []struct {
		name       string
		bucketName string
		objectKey  string
		endpoint   string
		want       runtime.Identity
	}{
		{
			name:       "no endpoint carries only the object path",
			bucketName: "my-bucket",
			objectKey:  "path/to/object.tar.gz",
			want: runtime.Identity{
				runtime.IdentityAttributeType: Type.String(),
				runtime.IdentityAttributePath: "my-bucket/path/to/object.tar.gz",
			},
		},
		{
			name:       "custom endpoint adds the url attributes",
			bucketName: "my-bucket",
			objectKey:  "path/to/object.tar.gz",
			endpoint:   "https://s3.example.com",
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "s3.example.com",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePath:     "my-bucket/path/to/object.tar.gz",
			},
		},
		{
			name:       "trailing slash on the endpoint is not doubled",
			bucketName: "my-bucket",
			objectKey:  "object",
			endpoint:   "https://s3.example.com/",
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "s3.example.com",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePath:     "my-bucket/object",
			},
		},
		{
			name:       "endpoint port is carried",
			bucketName: "my-bucket",
			objectKey:  "object",
			endpoint:   "https://minio.internal:9000",
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "minio.internal",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePort:     "9000",
				runtime.IdentityAttributePath:     "my-bucket/object",
			},
		},
		{
			name:       "empty object key identifies the bucket",
			bucketName: "my-bucket",
			want: runtime.Identity{
				runtime.IdentityAttributeType: Type.String(),
				runtime.IdentityAttributePath: "my-bucket",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IdentityFromObject(tt.bucketName, tt.objectKey, tt.endpoint)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIdentityFromObject_BucketNameRequired(t *testing.T) {
	_, err := IdentityFromObject("", "some/key", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucketName is required")
}

// TestIdentityFromObject_MatchesOnlyUnversionedConsumer pins the exact-match
// semantics: consumer identity types are compared by exact string, so only the
// unversioned spelling resolves. This mirrors OCIRegistry, HelmChartRepository and
// Wget, where the versioned spelling does not match either.
func TestIdentityFromObject_MatchesOnlyUnversionedConsumer(t *testing.T) {
	lookup, err := IdentityFromObject("my-bucket", "path/to/object.tar.gz", "")
	require.NoError(t, err)

	for _, tt := range []struct {
		consumerType string
		want         bool
	}{
		{"S3Bucket", true},
		{"S3Bucket/v1", false},
		{"s3bucket", false},
	} {
		t.Run(tt.consumerType, func(t *testing.T) {
			consumer := runtime.Identity{
				runtime.IdentityAttributeType: tt.consumerType,
				runtime.IdentityAttributePath: "my-bucket/path/to/object.tar.gz",
			}
			assert.Equal(t, tt.want, lookup.Clone().Match(consumer.Clone()))
		})
	}
}
