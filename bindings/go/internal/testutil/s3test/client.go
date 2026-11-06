package s3test

import (
	"bytes"
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/require"
)

// Region is the region [Client] signs requests for.
const Region = "us-east-1"

// Client returns an S3 client for the server at endpoint, a URL with scheme. It uses
// path-style addressing, [Region], [AccessKey] and [SecretKey], so it works against the
// server [Start] runs:
//
//	client := s3test.Client(t, "http://"+s3test.Start(t))
//	s3test.CreateBucket(t, ctx, client, "bucket")
//	s3test.PutObject(t, ctx, client, "bucket", "key", []byte("content"))
//
// The client ignores the AWS environment variables and shared config files, so a
// developer's AWS setup cannot redirect or re-sign the test requests.
func Client(t *testing.T, endpoint string) *s3.Client {
	t.Helper()
	return s3.New(s3.Options{
		Region:       Region,
		BaseEndpoint: aws.String(endpoint),
		UsePathStyle: true,
		Credentials:  credentials.NewStaticCredentialsProvider(AccessKey, SecretKey, ""),
	})
}

// CreateBucket creates bucket.
func CreateBucket(t *testing.T, ctx context.Context, client *s3.Client, bucket string) {
	t.Helper()
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)
}

// EnableVersioning turns on object versioning for bucket.
func EnableVersioning(t *testing.T, ctx context.Context, client *s3.Client, bucket string) {
	t.Helper()
	_, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket:                  aws.String(bucket),
		VersioningConfiguration: &types.VersioningConfiguration{Status: types.BucketVersioningStatusEnabled},
	})
	require.NoError(t, err)
}

// PutObject stores content under key in bucket and returns the version ID the server
// reported. The ID is empty when the bucket is not versioned.
func PutObject(t *testing.T, ctx context.Context, client *s3.Client, bucket, key string, content []byte) string {
	t.Helper()
	out, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content),
	})
	require.NoError(t, err)
	return aws.ToString(out.VersionId)
}
