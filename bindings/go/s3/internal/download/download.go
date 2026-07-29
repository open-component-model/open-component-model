// Package download contains the shared S3 download logic for the s3 bindings.
// Callers convert their own specification into a [Request] and invoke [Download],
// so client construction, credential handling and size limiting live in one place.
package download

import (
	"context"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

const (
	// defaultRegion is used when the request names none. AWS requires a region even
	// when a custom endpoint is targeted; S3-compatible stores usually ignore it.
	defaultRegion = "us-east-1"

	tempFilePattern = "ocm-s3-download-*"

	// UnversionedVersionID is the versionId S3 reports for objects that carry no real
	// version. It is a valid input to GetObject but pins nothing — overwriting such an
	// object leaves the versionId unchanged.
	UnversionedVersionID = "null"
)

// Result is the outcome of a [Download].
type Result struct {
	// Blob is the object body, backed by a file on disk that the blob owns; see [Blob].
	Blob *Blob
	// VersionID is the object version that was read, as reported by S3. It is
	// [UnversionedVersionID] for an unversioned object and empty for stores that do
	// not report a version, so it identifies immutable content only when it is neither.
	VersionID string
}

// Request describes a single S3 object download.
type Request struct {
	// Region is the bucket region. Optional: an empty one falls back to [defaultRegion],
	// which is what lets a custom endpoint work without naming a region.
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
// this call.
//
// The returned [Blob] owns that file: callers should [Blob.Close] it once they are
// done, and an unclosed blob has its file removed when it becomes unreachable.
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
		Bucket: new(req.BucketName),
		Key:    new(req.ObjectKey),
	}
	if req.Version != "" {
		in.VersionId = new(req.Version)
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

	// A store that reports no length, or lies about it, is caught while streaming. The
	// extra byte separates an object that exceeds the limit from one that just reaches
	// it; at MaxInt64 there is nothing to exceed, and incrementing would overflow to a
	// negative bound that io.LimitReader reads as "no bytes at all".
	body := io.Reader(out.Body)
	if maxDownloadSize > 0 {
		limit := maxDownloadSize
		if limit < math.MaxInt64 {
			limit++
		}
		body = io.LimitReader(out.Body, limit)
	}

	file, err := os.CreateTemp(o.TempDir, tempFilePattern)
	if err != nil {
		return nil, fmt.Errorf("error creating temporary file for s3 object %s/%s: %w", req.BucketName, req.ObjectKey, err)
	}
	path := file.Name()

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

	b, err := newBlob(path)
	if err != nil {
		return nil, fmt.Errorf("error creating blob for s3 object %s/%s from %s: %w", req.BucketName, req.ObjectKey, path, err)
	}
	b.SetMediaType(mediaType)

	return &Result{Blob: b, VersionID: aws.ToString(out.VersionId)}, nil
}

func httpConfig(cfg *httpv1alpha1.Config, insecureSkipTLSVerify bool) *httpv1alpha1.Config {
	out := cfg.DeepCopy()
	if out == nil {
		out = &httpv1alpha1.Config{}
	}

	if insecureSkipTLSVerify {
		out.InsecureSkipVerify = new(insecureSkipTLSVerify)
	}

	return out
}

// staticCredentials turns resolved S3 credentials into a static provider, or returns
// nil to leave the AWS default credential chain in charge. SigV4 signs with the key
// pair, and a session token only accompanies it, so a half-filled credential is
// rejected here rather than reaching S3 as an opaque 403.
func staticCredentials(creds *credv1.S3Credentials) (*credentials.StaticCredentialsProvider, error) {
	if creds == nil {
		return nil, nil
	}

	switch {
	case creds.AccessKeyID == "" && creds.SecretAccessKey == "" && creds.SessionToken == "":
		return nil, nil
	case creds.AccessKeyID == "":
		return nil, fmt.Errorf("incomplete s3 credentials: accessKeyId is required alongside the secret access key")
	case creds.SecretAccessKey == "":
		return nil, fmt.Errorf("incomplete s3 credentials: secretAccessKey is required alongside the access key id")
	}

	provider := credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken)
	return &provider, nil
}

// newClient builds an S3 client from the request and the download options. When no
// credentials are supplied, the AWS default credential chain is used.
func newClient(ctx context.Context, req Request, o *option) (*s3.Client, error) {
	region := req.Region
	if region == "" {
		region = defaultRegion
	}

	httpClient := o.HTTPClient
	if httpClient == nil {
		httpClient = ocmhttp.New(ocmhttp.WithConfig(httpConfig(o.HTTPConfig, req.InsecureSkipTLSVerify)))
	}

	// RetryMaxAttempts and RetryMode keep the SDK defaults and are configured the AWS
	// way, through AWS_MAX_ATTEMPTS and AWS_RETRY_MODE. The ocm HTTP config's retry
	// bounds transport round trips, a different unit than an SDK operation attempt; how
	// the two layers compose is described in the package documentation of the s3 module.
	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
		config.WithHTTPClient(httpClient),
	}

	if o.Credentials != nil {
		s3creds, err := credv1.ConvertToS3Credentials(o.Credentials)
		if err != nil {
			return nil, fmt.Errorf("error converting s3 credentials: %w", err)
		}
		provider, err := staticCredentials(s3creds)
		if err != nil {
			return nil, fmt.Errorf("error getting static s3 credentials: %w", err)
		}
		if provider != nil {
			loadOpts = append(loadOpts, config.WithCredentialsProvider(*provider))
		}
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("error loading aws config: %w", err)
	}

	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if req.Endpoint != "" {
			o.BaseEndpoint = new(req.Endpoint)
		}
		o.UsePathStyle = req.UsePathStyle
	}), nil
}
