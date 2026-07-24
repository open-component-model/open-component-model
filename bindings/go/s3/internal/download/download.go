// Package download contains the shared S3 download logic for the s3 bindings.
// Callers convert their own specification into a [Request] and invoke [Download],
// so client construction, credential handling and size limiting live in one place.
package download

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

// defaultRegion is used when no region is set. AWS requires a region even when a
// custom endpoint is targeted; S3-compatible stores usually ignore it.
const defaultRegion = "us-east-1"

// Request describes a single S3 object download.
type Request struct {
	// Region is the bucket region. Optional for custom endpoints.
	Region string
	// BucketName is the bucket holding the object.
	BucketName string
	// ObjectKey is the key of the object within the bucket.
	ObjectKey string
	// MediaType overrides the media type of the resulting blob. When empty the
	// object's Content-Type is used, falling back to application/octet-stream.
	MediaType string
	// Version pins a specific S3 object version (versionId). Empty reads the latest.
	Version string
	// Endpoint is the base endpoint of an S3-compatible store. Empty targets AWS.
	Endpoint string
	// UsePathStyle enables path-style addressing.
	UsePathStyle bool
	// InsecureSkipTLSVerify disables TLS verification for the endpoint.
	InsecureSkipTLSVerify bool
}

// Download fetches the object described by req and returns its body as an
// in-memory blob. The S3 client, credentials and maximum size are supplied via
// options; see [WithClient], [WithCredentials] and [WithMaxDownloadSize].
func Download(ctx context.Context, req Request, opts ...Option) (blob.ReadOnlyBlob, error) {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}

	if req.BucketName == "" {
		return nil, fmt.Errorf("bucketName is required")
	}
	if req.ObjectKey == "" {
		return nil, fmt.Errorf("objectKey is required")
	}

	getter := o.Client
	if getter == nil {
		client, err := newClient(ctx, req, o.Credentials)
		if err != nil {
			return nil, err
		}
		getter = client
	}

	in := &s3.GetObjectInput{
		Bucket: aws.String(req.BucketName),
		Key:    aws.String(req.ObjectKey),
	}
	if req.Version != "" {
		in.VersionId = aws.String(req.Version)
	}

	out, err := getter.GetObject(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("error getting s3 object %s/%s: %w", req.BucketName, req.ObjectKey, err)
	}
	defer func() { _ = out.Body.Close() }()

	maxDownloadSize := DefaultMaxDownloadSize
	if o.MaxDownloadSize != nil {
		maxDownloadSize = *o.MaxDownloadSize
	}

	var data []byte
	if maxDownloadSize > 0 {
		limitedReader := io.LimitReader(out.Body, maxDownloadSize+1)
		data, err = io.ReadAll(limitedReader)
		if err != nil {
			return nil, fmt.Errorf("error reading s3 object body from %s/%s: %w", req.BucketName, req.ObjectKey, err)
		}
		if int64(len(data)) > maxDownloadSize {
			return nil, fmt.Errorf("s3 object %s/%s exceeds maximum allowed size of %d bytes", req.BucketName, req.ObjectKey, maxDownloadSize)
		}
	} else {
		data, err = io.ReadAll(out.Body)
		if err != nil {
			return nil, fmt.Errorf("error reading s3 object body from %s/%s: %w", req.BucketName, req.ObjectKey, err)
		}
	}

	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = aws.ToString(out.ContentType)
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	return inmemory.New(bytes.NewReader(data),
		inmemory.WithMediaType(mediaType),
		inmemory.WithSize(int64(len(data))),
	), nil
}

// awsStaticCredentials returns a static AWS credentials provider for the given S3
// credentials, wiring the session token when set. It returns nil when there is no
// access key id, in which case the AWS default credential chain is used: a bare
// session token is not a usable credential on its own.
func awsStaticCredentials(s3creds *credv1.S3Credentials) aws.CredentialsProvider {
	if s3creds == nil || s3creds.AccessKeyID == "" {
		return nil
	}
	return credentials.NewStaticCredentialsProvider(s3creds.AccessKeyID, s3creds.SecretAccessKey, s3creds.SessionToken)
}

// newClient builds an S3 client from the request and OCM credentials. When no
// static credentials are supplied, the AWS default credential chain is used.
func newClient(ctx context.Context, req Request, creds runtime.Typed) (*s3.Client, error) {
	region := req.Region
	if region == "" {
		region = defaultRegion
	}

	loadOpts := []func(*config.LoadOptions) error{config.WithRegion(region)}

	if creds != nil {
		s3creds, err := credv1.ConvertToS3Credentials(creds)
		if err != nil {
			return nil, fmt.Errorf("error converting s3 credentials: %w", err)
		}
		if provider := awsStaticCredentials(s3creds); provider != nil {
			loadOpts = append(loadOpts, config.WithCredentialsProvider(provider))
		}
	}

	if req.InsecureSkipTLSVerify {
		httpClient := awshttp.NewBuildableClient().WithTransportOptions(func(tr *http.Transport) {
			if tr.TLSClientConfig == nil {
				tr.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
			}
			tr.TLSClientConfig.InsecureSkipVerify = true
		})
		loadOpts = append(loadOpts, config.WithHTTPClient(httpClient))
	}

	cfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("error loading aws config: %w", err)
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		if req.Endpoint != "" {
			o.BaseEndpoint = aws.String(req.Endpoint)
		}
		o.UsePathStyle = req.UsePathStyle
	}), nil
}
