package integration_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/minio"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/s3/repository"
	accessspec "ocm.software/open-component-model/bindings/go/s3/spec/access"
	accessv1 "ocm.software/open-component-model/bindings/go/s3/spec/access/v1"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

const minioImage = "minio/minio:RELEASE.2024-01-16T16-07-38Z"

// Test_Integration_S3 spins up a MinIO container, seeds objects with the AWS SDK,
// then exercises the S3 ResourceRepository end to end against a real (S3-compatible) store.
func Test_Integration_S3(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	container, err := minio.Run(ctx, minioImage)
	r.NoError(err)
	t.Cleanup(func() { r.NoError(testcontainers.TerminateContainer(container)) })

	hostPort, err := container.ConnectionString(ctx)
	r.NoError(err)
	endpoint := "http://" + hostPort

	// setup talks to MinIO directly to create buckets and seed objects.
	setup := newSetupClient(t, ctx, endpoint, container.Username, container.Password)

	creds := &credv1.S3Credentials{
		Type:            credv1.S3CredentialsVersionedType,
		AccessKeyID:     container.Username,
		SecretAccessKey: container.Password,
	}
	repo := repository.NewResourceRepository()

	access := func(bucket, key, version string) *accessv1.S3 {
		return &accessv1.S3{
			Type:         accessspec.V1VersionedType,
			Region:       "us-east-1",
			BucketName:   bucket,
			ObjectKey:    key,
			Endpoint:     endpoint,
			UsePathStyle: true,
			Version:      version,
		}
	}
	resourceFor := func(a *accessv1.S3) *descriptor.Resource {
		res := &descriptor.Resource{}
		res.Access = a
		return res
	}

	t.Run("download and digest", func(t *testing.T) {
		r := require.New(t)
		const bucket, key = "download-bucket", "path/to/blob.txt"
		content := []byte("hello ocm from s3")
		createBucket(t, ctx, setup, bucket)
		putObject(t, ctx, setup, bucket, key, content)

		res := resourceFor(access(bucket, key, ""))

		b, err := repo.DownloadResource(ctx, res, creds)
		r.NoError(err)
		rc, err := b.ReadCloser()
		r.NoError(err)
		defer func() { _ = rc.Close() }()
		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.Equal(content, got)

		withDigest, err := repo.ProcessResourceDigest(ctx, res, creds)
		r.NoError(err)
		r.NotNil(withDigest.Digest)
		r.Equal(godigest.FromBytes(content).Encoded(), withDigest.Digest.Value)
		r.Equal("SHA-256", withDigest.Digest.HashAlgorithm)
	})

	t.Run("pinned object version", func(t *testing.T) {
		r := require.New(t)
		const bucket, key = "versioned-bucket", "blob"
		createBucket(t, ctx, setup, bucket)
		enableVersioning(t, ctx, setup, bucket)

		v1Content := []byte("version one")
		v1ID := putObjectReturningVersion(t, ctx, setup, bucket, key, v1Content)
		// overwrite the same key; without a versionId the latest would be returned.
		putObject(t, ctx, setup, bucket, key, []byte("version two"))

		b, err := repo.DownloadResource(ctx, resourceFor(access(bucket, key, v1ID)), creds)
		r.NoError(err)
		rc, err := b.ReadCloser()
		r.NoError(err)
		defer func() { _ = rc.Close() }()
		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.Equal(v1Content, got, "pinned versionId must return the exact original object")
	})

	t.Run("missing object errors", func(t *testing.T) {
		r := require.New(t)
		const bucket = "missing-bucket"
		createBucket(t, ctx, setup, bucket)

		_, err := repo.DownloadResource(ctx, resourceFor(access(bucket, "does/not/exist", "")), creds)
		r.Error(err)
	})
}

func newSetupClient(t *testing.T, ctx context.Context, endpoint, user, pass string) *s3.Client {
	t.Helper()
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(user, pass, "")),
	)
	require.NoError(t, err)
	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
}

func createBucket(t *testing.T, ctx context.Context, client *s3.Client, bucket string) {
	t.Helper()
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
}

func enableVersioning(t *testing.T, ctx context.Context, client *s3.Client, bucket string) {
	t.Helper()
	_, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket:                  aws.String(bucket),
		VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled},
	})
	require.NoError(t, err)
}

func putObject(t *testing.T, ctx context.Context, client *s3.Client, bucket, key string, content []byte) {
	t.Helper()
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content),
	})
	require.NoError(t, err)
}

func putObjectReturningVersion(t *testing.T, ctx context.Context, client *s3.Client, bucket, key string, content []byte) string {
	t.Helper()
	out, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content),
	})
	require.NoError(t, err)
	require.NotNil(t, out.VersionId)
	return aws.ToString(out.VersionId)
}
