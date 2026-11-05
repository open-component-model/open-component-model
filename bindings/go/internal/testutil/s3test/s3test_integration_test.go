package s3test_test

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/s3test"
)

func Test_Integration_Start(t *testing.T) {
	hostPort := s3test.Start(t)

	tests := []struct {
		name      string
		bucket    string
		secretKey string
		wantErr   bool
	}{
		{name: "exported keys", bucket: "accepted", secretKey: s3test.SecretKey},
		{name: "wrong secret key", bucket: "rejected", secretKey: "wrong-secret", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)

			client := s3.New(s3.Options{
				Region:       "us-east-1",
				BaseEndpoint: aws.String("http://" + hostPort),
				UsePathStyle: true,
				Credentials:  credentials.NewStaticCredentialsProvider(s3test.AccessKey, tt.secretKey, ""),
			})
			_, err := client.CreateBucket(t.Context(), &s3.CreateBucketInput{Bucket: aws.String(tt.bucket)})
			if tt.wantErr {
				r.Error(err)
				return
			}
			r.NoError(err)
		})
	}
}
