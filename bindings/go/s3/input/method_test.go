package input_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/bindings/go/s3/input"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	inputspec "ocm.software/open-component-model/bindings/go/s3/spec/input"
)

// fakeGetter is a stand-in S3 client returning canned object content, so input tests
// need no network or real bucket.
type fakeGetter struct {
	body        []byte
	contentType string
	gotInput    *s3.GetObjectInput
}

func (f *fakeGetter) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.gotInput = in
	out := &s3.GetObjectOutput{Body: io.NopCloser(bytes.NewReader(f.body))}
	if f.contentType != "" {
		out.ContentType = aws.String(f.contentType)
	}
	return out, nil
}

func s3InputResource(t *testing.T, spec map[string]any) *constructorruntime.Resource {
	t.Helper()
	raw, err := json.Marshal(spec)
	require.NoError(t, err)

	r := &constructorruntime.Resource{}
	r.Name = "test-resource"
	r.Version = "1.0.0"
	r.Type = "blob"
	r.Input = &runtime.Raw{
		Type: inputspec.V1VersionedType,
		Data: raw,
	}
	return r
}

func TestInput_GetResourceCredentialConsumerIdentity(t *testing.T) {
	method := &input.InputMethod{}

	id, err := method.GetResourceCredentialConsumerIdentity(context.Background(),
		s3InputResource(t, map[string]any{"bucketName": "my-bucket", "objectKey": "path/to/blob"}))
	require.NoError(t, err)
	require.Equal(t, accessspec.S3BucketConsumerType, id[runtime.IdentityAttributeType])
	require.Equal(t, "s3.amazonaws.com", id[runtime.IdentityAttributeHostname])
	require.Equal(t, "my-bucket/path/to/blob", id[runtime.IdentityAttributePath])

	// Missing object key is rejected.
	_, err = method.GetResourceCredentialConsumerIdentity(context.Background(),
		s3InputResource(t, map[string]any{"bucketName": "my-bucket"}))
	require.Error(t, err)
}

func TestInput_ProcessResource(t *testing.T) {
	content := []byte("hello from s3 input")
	fake := &fakeGetter{body: content, contentType: "text/plain"}
	method := &input.InputMethod{Client: fake}

	result, err := method.ProcessResource(context.Background(),
		s3InputResource(t, map[string]any{"bucketName": "my-bucket", "objectKey": "path/blob.txt", "version": "v-1"}), nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.ProcessedBlobData)
	require.Nil(t, result.ProcessedResource)

	rc, err := result.ProcessedBlobData.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, content, got)

	// The input spec fields are forwarded to GetObject, including the pinned version.
	require.Equal(t, "my-bucket", aws.ToString(fake.gotInput.Bucket))
	require.Equal(t, "path/blob.txt", aws.ToString(fake.gotInput.Key))
	require.Equal(t, "v-1", aws.ToString(fake.gotInput.VersionId))

	ma, ok := result.ProcessedBlobData.(blob.MediaTypeAware)
	require.True(t, ok)
	mt, known := ma.MediaType()
	require.True(t, known)
	require.Equal(t, "text/plain", mt)
}
