package download

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

var regionName = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`)

func responseRegion(err error) string {
	var response *smithyhttp.ResponseError
	if errors.As(err, &response) && response.Response != nil && response.Response.Response != nil {
		return response.Response.Header.Get("X-Amz-Bucket-Region")
	}
	return ""
}

// permanentRedirect reports whether err is S3 answering that the bucket lives in another
// region. code is the error code the operation reports for it: a HEAD response carries
// no body, so the SDK names its error after the status instead of S3's code.
func permanentRedirect(err error, code string) bool {
	var response *smithyhttp.ResponseError
	var api smithy.APIError
	return errors.As(err, &response) && response.HTTPStatusCode() == http.StatusMovedPermanently &&
		errors.As(err, &api) && api.ErrorCode() == code
}

func bucketRegion(ctx context.Context, client *s3.Client, bucket string, redirect error) (string, error) {
	region := responseRegion(redirect)
	if region == "" {
		out, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: new(bucket)})
		if err != nil {
			// HeadBucket can report the region even when permission to list the bucket
			// is denied. No object is fetched unless this hint passes validation below.
			region = responseRegion(err)
			if region == "" {
				return "", fmt.Errorf("discovering bucket region with HeadBucket: %w", err)
			}
		} else {
			region = aws.ToString(out.BucketRegion)
		}
	}
	if !regionName.MatchString(region) {
		return "", fmt.Errorf("missing or invalid bucket region hint %q", region)
	}
	// Do not interpret Location or construct URLs from a server-provided hint.
	// Validate a region-shaped name through the same SDK resolver used for S3.
	_, err := s3.NewDefaultEndpointResolverV2().ResolveEndpoint(ctx, s3.EndpointParameters{Region: new(region)})
	if err != nil {
		return "", fmt.Errorf("invalid bucket region hint %q: %w", region, err)
	}
	if region == client.Options().Region {
		return "", fmt.Errorf("bucket region hint %q matches the region already attempted", region)
	}
	return region, nil
}

func getObject(ctx context.Context, client *s3.Client, req Request, in *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	return inBucketRegion(ctx, client, req, "PermanentRedirect", func(optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
		return client.GetObject(ctx, in, optFns...)
	})
}

func headObject(ctx context.Context, client *s3.Client, req Request, in *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
	return inBucketRegion(ctx, client, req, "MovedPermanently", func(optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
		return client.HeadObject(ctx, in, optFns...)
	})
}

// inBucketRegion runs call and, on AWS, repeats it once in the bucket's region if S3
// answers with redirectCode that the bucket lives elsewhere.
func inBucketRegion[T any](ctx context.Context, client *s3.Client, req Request, redirectCode string, call func(...func(*s3.Options)) (T, error)) (T, error) {
	var none T
	out, err := call()
	if req.Endpoint != "" || !permanentRedirect(err, redirectCode) {
		return out, err
	}
	region, discoveryErr := bucketRegion(ctx, client, req.BucketName, err)
	if discoveryErr != nil {
		return none, errors.Join(err, discoveryErr)
	}
	// An operation override preserves credentials (including anonymous), transport,
	// addressing and retry configuration without rebuilding the client. Never loop.
	out, err = call(func(o *s3.Options) { o.Region = region })
	if err != nil {
		return none, fmt.Errorf("retrying in bucket region %q: %w", region, err)
	}
	return out, nil
}
