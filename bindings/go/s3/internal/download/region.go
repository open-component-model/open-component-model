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

// permanentRedirect reports whether S3 refused the request because the bucket lives in
// another region. A GET carries the PermanentRedirect code in its error body; a HEAD has
// no body, so the SDK derives the code from the 301 status text.
func permanentRedirect(err error) bool {
	var response *smithyhttp.ResponseError
	var api smithy.APIError
	if !errors.As(err, &response) || response.HTTPStatusCode() != http.StatusMovedPermanently || !errors.As(err, &api) {
		return false
	}
	switch api.ErrorCode() {
	case "PermanentRedirect", "MovedPermanently":
		return true
	default:
		return false
	}
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

// inBucketRegion runs call and, on AWS, repeats it once in the bucket's own region when
// S3 answers with a permanent redirect.
func inBucketRegion[T any](ctx context.Context, client *s3.Client, req Request, call func(...func(*s3.Options)) (T, error)) (T, error) {
	out, err := call()
	if req.Endpoint != "" || !permanentRedirect(err) {
		return out, err
	}
	var zero T
	region, discoveryErr := bucketRegion(ctx, client, req.BucketName, err)
	if discoveryErr != nil {
		return zero, errors.Join(err, discoveryErr)
	}
	// An operation override preserves credentials (including anonymous), transport,
	// addressing and retry configuration without rebuilding the client. Never loop.
	out, err = call(func(o *s3.Options) { o.Region = region })
	if err != nil {
		return zero, fmt.Errorf("retrying in bucket region %q: %w", region, err)
	}
	return out, nil
}
