// Package download contains the shared S3 download logic for the s3 bindings.
// Callers convert their own specification into a [Request] and invoke [Download],
// so client construction, credential handling and size limiting live in one place.
package download

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

// AWS requires a region even when a custom endpoint is targeted; S3-compatible
// stores usually ignore it.
const defaultRegion = "us-east-1"

const tempFilePattern = "ocm-s3-download-*"

// UnversionedVersionID is the versionId S3 reports for objects that carry no real
// version. It is a valid input to GetObject but pins nothing — overwriting such an
// object leaves the versionId unchanged.
const UnversionedVersionID = "null"

// Result is the outcome of a [Download].
type Result struct {
	// Blob is the object body, backed by a file on disk.
	Blob blob.ReadOnlyBlob
	// VersionID is the object version that was read, as reported by S3. It is
	// [UnversionedVersionID] for an unversioned object and empty for stores that do
	// not report a version, so it identifies immutable content only when it is neither.
	VersionID string
}

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

// Download fetches the object described by req and returns its body as a blob
// backed by a file on disk, along with the object version that was read. Objects
// are streamed rather than buffered, so memory use stays flat regardless of object
// size; the file is created under the directory given by [WithTempDir] and outlives
// this call, so the caller owns it.
//
// The S3 client, credentials and maximum size are supplied via options; see
// [WithClient], [WithCredentials] and [WithMaxDownloadSize].
func Download(ctx context.Context, req Request, opts ...Option) (_ *Result, err error) {
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
		client, err := newClient(ctx, req, o)
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

	// S3 reports the size up front, so an oversized object is rejected before any
	// of it is transferred.
	if maxDownloadSize > 0 && out.ContentLength != nil && *out.ContentLength > maxDownloadSize {
		return nil, fmt.Errorf("s3 object %s/%s exceeds maximum allowed size of %d bytes", req.BucketName, req.ObjectKey, maxDownloadSize)
	}

	body := io.Reader(out.Body)
	if maxDownloadSize > 0 {
		// One byte past the limit is enough to tell "at the limit" from "over" it.
		body = io.LimitReader(out.Body, maxDownloadSize+1)
	}

	file, err := os.CreateTemp(o.TempDir, tempFilePattern)
	if err != nil {
		return nil, fmt.Errorf("error creating temporary file for s3 object %s/%s: %w", req.BucketName, req.ObjectKey, err)
	}
	path := file.Name()
	// Only the returned blob gives the file an owner, so a failure below must remove it.
	defer func() {
		if err != nil {
			_ = os.Remove(path)
		}
	}()

	written, err := io.Copy(file, body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return nil, fmt.Errorf("error writing s3 object %s/%s to %s: %w", req.BucketName, req.ObjectKey, path, err)
	}

	if maxDownloadSize > 0 && written > maxDownloadSize {
		return nil, fmt.Errorf("s3 object %s/%s exceeds maximum allowed size of %d bytes", req.BucketName, req.ObjectKey, maxDownloadSize)
	}

	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = aws.ToString(out.ContentType)
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	b, err := filesystem.GetBlobFromOSPath(path)
	if err != nil {
		return nil, fmt.Errorf("error creating blob for s3 object %s/%s from %s: %w", req.BucketName, req.ObjectKey, path, err)
	}
	b.SetMediaType(mediaType)

	return &Result{Blob: b, VersionID: aws.ToString(out.VersionId)}, nil
}

// httpConfig folds the access spec's InsecureSkipTLSVerify into a copy of cfg, so
// the shared client applies it through its own transport and warns once per host
// instead of disabling verification silently. Retry passes through untouched; see
// the package documentation of the s3 module for how the two retry layers relate.
func httpConfig(cfg *httpv1alpha1.Config, insecureSkipTLSVerify bool) *httpv1alpha1.Config {
	out := cfg.DeepCopy()
	if out == nil {
		out = &httpv1alpha1.Config{}
	}

	if insecureSkipTLSVerify {
		skip := true
		out.InsecureSkipVerify = &skip
	}

	return out
}

// newClient builds an S3 client from the request and the download options. When no
// static credentials are supplied, the AWS default credential chain is used.
func newClient(ctx context.Context, req Request, o *option) (*s3.Client, error) {
	region := req.Region
	if region == "" {
		region = defaultRegion
	}

	httpClient := o.HTTPClient
	if httpClient == nil {
		httpClient = ocmhttp.New(ocmhttp.WithConfig(httpConfig(o.HTTPConfig, req.InsecureSkipTLSVerify)))
	}

	// Deliberately no retry option: the SDK resolves its own attempt count from
	// AWS_MAX_ATTEMPTS and AWS_RETRY_MODE.
	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
		config.WithHTTPClient(httpClient),
	}

	if o.Credentials != nil {
		s3creds, err := credv1.ConvertToS3Credentials(o.Credentials)
		if err != nil {
			return nil, fmt.Errorf("error converting s3 credentials: %w", err)
		}
		if s3creds.AccessKeyID != "" {
			loadOpts = append(loadOpts, config.WithCredentialsProvider(
				credentials.NewStaticCredentialsProvider(s3creds.AccessKeyID, s3creds.SecretAccessKey, s3creds.SessionToken),
			))
		}
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("error loading aws config: %w", err)
	}

	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if req.Endpoint != "" {
			o.BaseEndpoint = aws.String(req.Endpoint)
		}
		o.UsePathStyle = req.UsePathStyle
	}), nil
}
