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
			name:       "a bucket on its own is a valid location",
			bucketName: "my-bucket",
			want: runtime.Identity{
				runtime.IdentityAttributeType: Type.String(),
				runtime.IdentityAttributePath: "my-bucket",
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
			name:       "endpoint path takes no part in the object path",
			bucketName: "my-bucket",
			objectKey:  "path/to/object.tar.gz",
			endpoint:   "https://gw.example.com/gateway",
			want: runtime.Identity{
				runtime.IdentityAttributeType:     Type.String(),
				runtime.IdentityAttributeHostname: "gw.example.com",
				runtime.IdentityAttributeScheme:   "https",
				runtime.IdentityAttributePath:     "my-bucket/path/to/object.tar.gz",
			},
		},
		{
			name:       "endpoint path with a trailing slash takes no part either",
			bucketName: "my-bucket",
			objectKey:  "object",
			endpoint:   "https://minio.internal:9000/gateway/",
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

// Keys that look like cleanable paths name real, distinct objects and must reach the
// identity byte for byte.
func TestIdentityFromObject_KeysAreNotCleaned(t *testing.T) {
	for _, tt := range []struct {
		name      string
		objectKey string
		want      string
	}{
		{name: "empty segment", objectKey: "asdf//asdf", want: "my-bucket/asdf//asdf"},
		{name: "single dot segment", objectKey: "asdf/./asdf", want: "my-bucket/asdf/./asdf"},
		{name: "double dot segment", objectKey: "asdf/../asdf", want: "my-bucket/asdf/../asdf"},
		{name: "trailing slash", objectKey: "asdf/", want: "my-bucket/asdf/"},
		{name: "leading slash", objectKey: "/asdf", want: "my-bucket//asdf"},
		{name: "dot file", objectKey: ".hidden", want: "my-bucket/.hidden"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IdentityFromObject("my-bucket", tt.objectKey, "")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got[runtime.IdentityAttributePath])

			// The endpoint path is built separately, so it is checked separately.
			got, err = IdentityFromObject("my-bucket", tt.objectKey, "https://s3.example.com")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got[runtime.IdentityAttributePath])
		})
	}
}

func TestIdentityFromObject_EndpointBasePathIsNotPartOfTheIdentity(t *testing.T) {
	got, err := IdentityFromObject("my-bucket", "object", "https://gateway.example.com/s3")
	require.NoError(t, err)
	assert.Equal(t, runtime.Identity{
		runtime.IdentityAttributeType:     Type.String(),
		runtime.IdentityAttributeHostname: "gateway.example.com",
		runtime.IdentityAttributeScheme:   "https",
		runtime.IdentityAttributePath:     "my-bucket/object",
	}, got)
}

// Characters a URL would treat specially stay literal in a key.
func TestIdentityFromObject_KeyIsNotURLDecoded(t *testing.T) {
	for _, objectKey := range []string{"a%2Fb", "report?draft", "notes#1", "a b"} {
		t.Run(objectKey, func(t *testing.T) {
			got, err := IdentityFromObject("my-bucket", objectKey, "https://s3.example.com")
			require.NoError(t, err)
			assert.Equal(t, "my-bucket/"+objectKey, got[runtime.IdentityAttributePath])
		})
	}
}

func TestIdentityFromObject_BucketNameRequired(t *testing.T) {
	_, err := IdentityFromObject("", "some/key", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bucketName is required")
}

// Consumer identity types are compared by exact string, so only the unversioned spelling
// resolves — as in OCIRegistry, HelmChartRepository and Wget.
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

// A consumer entry names the object as bucketName/objectKey, whatever base path the
// endpoint carries: an entry that had to spell out the endpoint's path prefix would
// neither be portable between stores nor match what ocmv1 resolves.
func TestIdentityFromObject_ConsumerEntryIsIndependentOfEndpointPath(t *testing.T) {
	for _, endpoint := range []string{
		"https://gw.example.com",
		"https://gw.example.com/gateway",
		"https://gw.example.com/gateway/nested/",
	} {
		t.Run(endpoint, func(t *testing.T) {
			lookup, err := IdentityFromObject("my-bucket", "object.tar.gz", endpoint)
			require.NoError(t, err)

			for _, tt := range []struct {
				name string
				path string
			}{
				{name: "exact object", path: "my-bucket/object.tar.gz"},
				{name: "any object in the bucket", path: "my-bucket/*"},
				{name: "no path at all", path: ""},
			} {
				t.Run(tt.name, func(t *testing.T) {
					consumer := runtime.Identity{
						runtime.IdentityAttributeType:     Type.String(),
						runtime.IdentityAttributeHostname: "gw.example.com",
					}
					if tt.path != "" {
						consumer[runtime.IdentityAttributePath] = tt.path
					}
					assert.True(t, lookup.Clone().Match(consumer.Clone()))
				})
			}
		})
	}
}
