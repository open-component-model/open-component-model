// Package download contains the shared S3 download logic for the s3 bindings.
// Callers convert their own specification into a [Request] and invoke [Download],
// so client construction, credential handling and size limiting live in one place.
package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/url"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

const (
	// defaultRegion is used when neither the request nor the AWS environment names one.
	// AWS requires a region even when a custom endpoint is targeted; S3-compatible stores
	// usually ignore it.
	defaultRegion = "us-east-1"

	tempFilePattern = "ocm-s3-download-*"

	// UnversionedVersionID is the versionId S3 reports for objects that carry no real
	// version. It is a valid input to GetObject but pins nothing — overwriting such an
	// object leaves the versionId unchanged.
	UnversionedVersionID = "null"
)

// Result is the outcome of a [Download].
type Result struct {
	// Blob is the object body, backed by a file on disk.
	Blob *filesystem.Blob
	// VersionID is the object version that was read, as reported by S3. It is
	// [UnversionedVersionID] for an unversioned object and empty for stores that do
	// not report a version, so it identifies immutable content only when it is neither.
	VersionID string
}

// Request describes a single S3 object download.
type Request struct {
	// Region is the bucket region. Optional: an empty one is resolved by the AWS SDK
	// from AWS_REGION or the shared config, falling back to [defaultRegion], which is
	// what lets a custom endpoint work without naming a region.
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
}

// Download fetches the object described by req and returns its body as a blob
// backed by a file on disk, along with the object version that was read. Objects
// are streamed rather than buffered, so memory use stays flat regardless of object
// size; the file is created under the directory given by [WithTempDir], outlives
// this call and is owned by the caller.
//
// The credentials and maximum size are supplied via options; see [WithCredentials]
// and [WithMaxDownloadSize].
func Download(ctx context.Context, req Request, opts ...Option) (*Result, error) {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}

	if req.BucketName == "" {
		return nil, errors.New("bucketName is required")
	}
	if req.ObjectKey == "" {
		return nil, errors.New("objectKey is required")
	}

	getter, err := newClient(ctx, req, o)
	if err != nil {
		return nil, err
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
	// extra byte separates exceeding the limit from reaching it; MaxInt64 cannot be
	// exceeded and incrementing it would overflow into a negative, empty bound.
	body := io.Reader(out.Body)
	if maxDownloadSize > 0 {
		limit := maxDownloadSize
		if limit < math.MaxInt64 {
			limit++
		}
		body = io.LimitReader(out.Body, limit)
	}

	mediaType := req.MediaType
	if mediaType == "" {
		mediaType = aws.ToString(out.ContentType)
	}
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}

	file, err := os.CreateTemp(o.TempDir, tempFilePattern)
	if err != nil {
		return nil, fmt.Errorf("error creating temporary file for s3 object %s/%s: %w", req.BucketName, req.ObjectKey, err)
	}

	b, err := storeObject(file, body, maxDownloadSize, req)
	if err != nil {
		if err := os.Remove(file.Name()); err != nil {
			slog.WarnContext(ctx, "error removing temporary file after failed download", "file", file.Name(), "err", err)
		}
		return nil, err
	}
	b.SetMediaType(mediaType)

	return &Result{Blob: b, VersionID: aws.ToString(out.VersionId)}, nil
}

// storeObject streams body into file and returns a blob backed by it. It closes file
// whether or not it succeeds, but never removes it; the caller does that in one place.
func storeObject(file *os.File, body io.Reader, maxDownloadSize int64, req Request) (*filesystem.Blob, error) {
	path := file.Name()

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

	b, err := filesystem.GetBlobFromOSPath(path)
	if err != nil {
		return nil, fmt.Errorf("error creating blob for s3 object %s/%s from %s: %w", req.BucketName, req.ObjectKey, path, err)
	}

	return b, nil
}

// disableRetry is the maxRetries value that turns the ocm transport's retry off.
// The [httpv1alpha1.RetryConfig] scale is -1 to disable, 0 for infinite, positive for a
// count of retries after the initial request.
const disableRetry = -1

func httpConfig(cfg *httpv1alpha1.Config) *httpv1alpha1.Config {
	out := cfg.DeepCopy()
	if out == nil {
		out = &httpv1alpha1.Config{}
	}

	// Retrying is left to the SDK: it retries the whole operation, re-signs every attempt
	// and classifies S3's error codes, and retrying here too would multiply both counts.
	// Per-host entries override the global policy, so they are switched off as well.
	out.Retry = &httpv1alpha1.RetryConfig{MaxRetries: new(disableRetry)}
	for host, hostCfg := range out.Hosts {
		if hostCfg == nil {
			continue
		}
		hostCfg.Retry = &httpv1alpha1.RetryConfig{MaxRetries: new(disableRetry)}
		out.Hosts[host] = hostCfg
	}

	return out
}

// effectiveRetry returns the retry policy cfg puts in effect for endpoint. An endpoint
// that names no host, and one the SDK targets by default (AWS), resolve to the global
// policy.
func effectiveRetry(cfg *httpv1alpha1.Config, endpoint string) *httpv1alpha1.RetryConfig {
	var host string
	if u, err := url.Parse(endpoint); err == nil {
		host = u.Host
	}

	return cfg.ResolveHost(host).Retry
}

// sdkRetryAttempts translates the ocm retry configuration that applies to endpoint into
// the SDK's attempt count.
func sdkRetryAttempts(cfg *httpv1alpha1.Config, endpoint string) int {
	retryCfg := effectiveRetry(cfg, endpoint)
	if retryCfg == nil || retryCfg.MaxRetries == nil {
		return 0
	}

	switch n := *retryCfg.MaxRetries; n {
	case disableRetry:
		return 1
	case 0:
		return math.MaxInt
	default:
		return n + 1
	}
}

// staticCredentials turns resolved S3 credentials into a static provider, or returns nil
// to leave the AWS default credential chain in charge, which is how an in-cluster setup
// reaches S3 without key material in the OCM configuration.
//
// Credentials that set any field at all are handed to the SDK as given, even a combination
// it will refuse: validating them here would duplicate the SDK's rules, and dropping them
// silently would send the request under whatever identity the default chain resolves.
func staticCredentials(creds *credv1.S3Credentials) *credentials.StaticCredentialsProvider {
	if creds == nil || (creds.AccessKeyID == "" && creds.SecretAccessKey == "" && creds.SessionToken == "") {
		return nil
	}

	provider := credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, creds.SessionToken)
	return &provider
}

// newClient builds an S3 client from the request and the download options. When no
// credentials are supplied, the AWS default credential chain is used.
func newClient(ctx context.Context, req Request, o *option) (*s3.Client, error) {
	httpClient := o.HTTPClient
	if httpClient == nil {
		httpClient = ocmhttp.New(ocmhttp.WithConfig(httpConfig(o.HTTPConfig)))
	}

	// Retrying happens in the SDK alone, driven by the ocm retry configuration; see
	// [httpConfig]. An unset one leaves the SDK on its own default.
	loadOpts := []func(*config.LoadOptions) error{
		config.WithHTTPClient(httpClient),
	}
	// A region named in the specification wins. Leaving it out lets the SDK resolve one
	// the way every other AWS tool does, from AWS_REGION or the shared config profile;
	// [defaultRegion] only fills in when that finds nothing either.
	if req.Region != "" {
		loadOpts = append(loadOpts, config.WithRegion(req.Region))
	}
	if attempts := sdkRetryAttempts(o.HTTPConfig, req.Endpoint); attempts > 0 {
		loadOpts = append(loadOpts, config.WithRetryMaxAttempts(attempts))
	}

	if o.Credentials != nil {
		s3creds, err := credv1.ConvertToS3Credentials(o.Credentials)
		if err != nil {
			return nil, fmt.Errorf("error converting s3 credentials: %w", err)
		}
		if provider := staticCredentials(s3creds); provider != nil {
			loadOpts = append(loadOpts, config.WithCredentialsProvider(*provider))
		}
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("error loading aws config: %w", err)
	}
	if awsCfg.Region == "" {
		awsCfg.Region = defaultRegion
	}

	return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if req.Endpoint != "" {
			o.BaseEndpoint = new(req.Endpoint)
		}
		o.UsePathStyle = req.UsePathStyle
	}), nil
}
