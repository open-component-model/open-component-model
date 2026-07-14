package repository

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	v1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
)

// fakeGetter is a stand-in S3 client that returns canned object content and records
// the input it was called with, so download tests need no network or real bucket.
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

func s3Resource(spec *v1.S3) *descriptor.Resource {
	spec.Type = accessspec.V1VersionedType
	r := &descriptor.Resource{}
	r.Access = spec
	return r
}

func Test_GetResourceCredentialConsumerIdentity(t *testing.T) {
	repo := NewResourceRepository()

	// AWS (no endpoint): host defaults to the AWS S3 host and the path carries bucket/objectKey.
	id, err := repo.GetResourceCredentialConsumerIdentity(context.Background(),
		s3Resource(&v1.S3{BucketName: "my-bucket", ObjectKey: "path/to/blob", Region: "eu-central-1"}))
	require.NoError(t, err)
	require.Equal(t, "https", id[runtime.IdentityAttributeScheme])
	require.Equal(t, awsDefaultHost, id[runtime.IdentityAttributeHostname])
	require.Equal(t, "my-bucket/path/to/blob", id[runtime.IdentityAttributePath])
	require.Equal(t, accessspec.S3BucketConsumerType, id[runtime.IdentityAttributeType])

	// No object key: the path is just the bucket.
	id, err = repo.GetResourceCredentialConsumerIdentity(context.Background(),
		s3Resource(&v1.S3{BucketName: "my-bucket"}))
	require.NoError(t, err)
	require.Equal(t, "my-bucket", id[runtime.IdentityAttributePath])

	// Custom endpoint: host, port and path come from the endpoint plus bucket/objectKey.
	id, err = repo.GetResourceCredentialConsumerIdentity(context.Background(),
		s3Resource(&v1.S3{BucketName: "b", ObjectKey: "obj", Endpoint: "https://minio.internal:9000"}))
	require.NoError(t, err)
	require.Equal(t, "minio.internal", id[runtime.IdentityAttributeHostname])
	require.Equal(t, "9000", id[runtime.IdentityAttributePort])
	require.Equal(t, "b/obj", id[runtime.IdentityAttributePath])

	// Missing bucket and nil resource are rejected.
	_, err = repo.GetResourceCredentialConsumerIdentity(context.Background(), s3Resource(&v1.S3{}))
	require.Error(t, err)
	_, err = repo.GetResourceCredentialConsumerIdentity(context.Background(), nil)
	require.Error(t, err)
}

func Test_DownloadResource(t *testing.T) {
	content := []byte("hello from s3")
	fake := &fakeGetter{body: content, contentType: "text/plain"}
	repo := NewResourceRepository(WithClient(fake))

	b, err := repo.DownloadResource(context.Background(),
		s3Resource(&v1.S3{BucketName: "my-bucket", ObjectKey: "path/blob.txt", Version: "v-1"}), nil)
	require.NoError(t, err)

	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.Equal(t, content, got)

	// The access spec fields are forwarded to GetObject, including the pinned version.
	require.Equal(t, "my-bucket", aws.ToString(fake.gotInput.Bucket))
	require.Equal(t, "path/blob.txt", aws.ToString(fake.gotInput.Key))
	require.Equal(t, "v-1", aws.ToString(fake.gotInput.VersionId))
}

func Test_ProcessResourceDigest(t *testing.T) {
	content := []byte("digest me")
	repo := NewResourceRepository(WithClient(&fakeGetter{body: content}))

	res, err := repo.ProcessResourceDigest(context.Background(),
		s3Resource(&v1.S3{BucketName: "b", ObjectKey: "k"}), nil)
	require.NoError(t, err)
	require.NotNil(t, res.Digest)
	require.Equal(t, godigest.FromBytes(content).Encoded(), res.Digest.Value)
	require.Equal(t, hashAlgorithmSHA256, res.Digest.HashAlgorithm)
	require.Equal(t, genericBlobDigestV1, res.Digest.NormalisationAlgorithm)
}
