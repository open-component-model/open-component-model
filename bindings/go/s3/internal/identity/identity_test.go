package identity

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
)

func TestConsumer_AWSDefault(t *testing.T) {
	id, err := Consumer("", "my-bucket", "path/to/blob")
	require.NoError(t, err)
	require.Equal(t, accessspec.S3BucketConsumerType, id[runtime.IdentityAttributeType])
	require.Equal(t, "https", id[runtime.IdentityAttributeScheme])
	require.Equal(t, "s3.amazonaws.com", id[runtime.IdentityAttributeHostname])
	require.Equal(t, "my-bucket/path/to/blob", id[runtime.IdentityAttributePath])
	require.Empty(t, id[runtime.IdentityAttributePort])
}

func TestConsumer_BucketOnly(t *testing.T) {
	id, err := Consumer("", "my-bucket", "")
	require.NoError(t, err)
	require.Equal(t, "my-bucket", id[runtime.IdentityAttributePath])
}

func TestConsumer_Endpoint(t *testing.T) {
	id, err := Consumer("https://minio.internal:9000", "b", "obj")
	require.NoError(t, err)
	require.Equal(t, "minio.internal", id[runtime.IdentityAttributeHostname])
	require.Equal(t, "9000", id[runtime.IdentityAttributePort])
	require.Equal(t, "b/obj", id[runtime.IdentityAttributePath])
	require.Equal(t, accessspec.S3BucketConsumerType, id[runtime.IdentityAttributeType])
}

func TestConsumer_EndpointTrailingSlash(t *testing.T) {
	// A trailing slash on the endpoint must not produce a doubled slash in the path.
	id, err := Consumer("http://localhost:9000/", "b", "obj")
	require.NoError(t, err)
	require.Equal(t, "http", id[runtime.IdentityAttributeScheme])
	require.Equal(t, "b/obj", id[runtime.IdentityAttributePath])
}
