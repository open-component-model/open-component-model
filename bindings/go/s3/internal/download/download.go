// Package download contains the shared S3 download logic for the s3 bindings.
// Callers convert their own specification into a [Request] and either [Stream] the
// object once or wrap it in a lazy [Blob] with [NewBlob], so client construction,
// credential handling and size limiting live in one place. Nothing is stored: every
// read is a GetObject whose body is handed through.
package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/opencontainers/go-digest"

	"ocm.software/open-component-model/bindings/go/blob"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

const (
	// defaultRegion applies when neither the request nor the AWS environment names one.
	// AWS needs a region even for a custom endpoint. Most S3-compatible stores ignore
	// it.
	defaultRegion = "us-east-1"

	// defaultMediaType is reported for an object that neither the request nor the
	// store gives a media type.
	defaultMediaType = "application/octet-stream"

	// UnversionedVersionID is the versionId S3 reports for objects that carry no real
	// version. It is a valid input to GetObject but pins nothing — overwriting such an
	// object leaves the versionId unchanged.
	UnversionedVersionID = "null"
)

// errTooLarge marks an object rejected by the download size limit.
var errTooLarge = errors.New("exceeds maximum allowed size")

// Result describes the object a [Stream] wrote.
type Result struct {
	// VersionID is the object version that was read, as reported by S3. It is
	// [UnversionedVersionID] for an unversioned object and empty for stores that do
	// not report a version, so it identifies immutable content only when it is neither.
	VersionID string
	// Digest is the digest of the object, computed while it was streamed; empty
	// without [WithDigestAlgorithm].
	Digest digest.Digest
	// Size is the number of bytes written.
	Size int64
	// MediaType is the media type of the object; see [Request.MediaType].
	MediaType string
}

// Request describes a single S3 object download.
type Request struct {
	// Region is the bucket region. It is optional: if it is empty, the AWS SDK reads
	// AWS_REGION or the shared config, and falls back to [defaultRegion]. This is why a
	// custom endpoint works without a region.
	Region string
	// BucketName is the bucket holding the object.
	BucketName string
	// ObjectKey is the key of the object within the bucket.
	ObjectKey string
	// MediaType overrides the media type of the object. When empty the object's
	// Content-Type is used, falling back to application/octet-stream.
	MediaType string
	// Version pins a specific S3 object version (versionId). Empty reads the latest.
	Version string
	// Endpoint is the base endpoint of an S3-compatible store. Empty targets AWS.
	Endpoint string
	// UsePathStyle enables path-style addressing.
	UsePathStyle bool
}

// IsVersion reports whether versionID, as S3 reports it, names an immutable object
// version: stores that report none and unversioned objects pin nothing.
func IsVersion(versionID string) bool {
	return versionID != "" && versionID != UnversionedVersionID
}

// Stream fetches the object described by req and writes its body to w as it
// arrives. Nothing is stored, so memory use stays flat regardless of object size.
//
// The credentials and maximum size are supplied via options; see [WithCredentials]
// and [WithMaxDownloadSize].
func Stream(ctx context.Context, req Request, w io.Writer, opts ...Option) (*Result, error) {
	o := newOptions(opts)

	obj, err := open(ctx, req, o)
	if err != nil {
		return nil, err
	}
	defer func() { _ = obj.body.Close() }()

	var digester digest.Digester
	if o.DigestAlgorithm != "" {
		digester = o.DigestAlgorithm.Digester()
		w = io.MultiWriter(w, digester.Hash())
	}

	written, err := io.Copy(w, obj.body)
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return nil, err
		}
		return nil, fmt.Errorf("error streaming s3 object %s/%s: %w", req.BucketName, req.ObjectKey, err)
	}

	result := &Result{VersionID: obj.versionID, Size: written, MediaType: obj.mediaType}
	if digester != nil {
		result.Digest = digester.Digest()
	}
	return result, nil
}

// Blob is an S3 object as a lazy blob: every read sends a GetObject and hands its
// body through, so nothing is stored. The first response that reports a real object
// version pins it, so later reads serve the same object even if the key has been
// overwritten meanwhile; an unversioned object is read at its latest state every time.
// Reads are bound to the context the blob was created with.
//
// Size and media type are learned once: from the first response, or, if they are asked
// for before any read, from a HeadObject. A failed HeadObject is not repeated and leaves
// them unknown until a read succeeds; until then Size reports [blob.SizeUnknown] and
// MediaType reports no media type, unless the request sets one.
type Blob struct {
	ctx context.Context
	req Request
	o   *option

	mu        sync.Mutex
	version   string
	described bool
	headed    bool
	size      int64
	mediaType string
}

var (
	_ blob.ReadOnlyBlob   = (*Blob)(nil)
	_ blob.SizeAware      = (*Blob)(nil)
	_ blob.MediaTypeAware = (*Blob)(nil)
)

// NewBlob returns the object described by req as a lazy [Blob]. Errors of the
// download surface on read.
func NewBlob(ctx context.Context, req Request, opts ...Option) *Blob {
	return &Blob{ctx: ctx, req: req, o: newOptions(opts), size: blob.SizeUnknown}
}

// ReadCloser requests the object and returns its body. Reading fails once the body
// exceeds the maximum download size.
func (b *Blob) ReadCloser() (io.ReadCloser, error) {
	obj, err := open(b.ctx, b.request(), b.o)
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.version == "" && IsVersion(obj.versionID) {
		b.version = obj.versionID
	}
	if !b.described {
		b.describe(obj.size, obj.mediaType)
	}
	return obj.body, nil
}

// Size returns the size of the object, or [blob.SizeUnknown] if it cannot be learned.
func (b *Blob) Size() int64 {
	if !b.learn() {
		return blob.SizeUnknown
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size
}

// MediaType returns the media type of the request, otherwise that of the object.
func (b *Blob) MediaType() (string, bool) {
	if b.req.MediaType != "" {
		return b.req.MediaType, true
	}
	if !b.learn() {
		return "", false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.mediaType, true
}

// learn reports whether size and media type are known, asking the store with a
// HeadObject if no read has told them yet. The lock is held across the request so
// concurrent callers do not each send one.
func (b *Blob) learn() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.described || b.headed {
		return b.described
	}
	b.headed = true

	req := b.req
	if req.Version == "" {
		req.Version = b.version
	}
	obj, err := head(b.ctx, req, b.o)
	if err != nil {
		return false
	}
	// Pin the described version so reads serve the bytes the size and media type describe.
	if b.version == "" && IsVersion(obj.versionID) {
		b.version = obj.versionID
	}
	b.describe(obj.size, obj.mediaType)
	return true
}

// describe records what a response reported about the object. b.mu must be held.
func (b *Blob) describe(size int64, mediaType string) {
	b.described = true
	b.size = size
	b.mediaType = mediaType
}

// request returns the request, pinned to the version of an earlier read if there was one.
func (b *Blob) request() Request {
	b.mu.Lock()
	defer b.mu.Unlock()
	req := b.req
	if req.Version == "" {
		req.Version = b.version
	}
	return req
}

// object is one GetObject response, or a HeadObject response without a body.
type object struct {
	// body is the object body, failing once it exceeds the maximum download size.
	body io.ReadCloser
	// versionID is the version S3 reported; see [Result.VersionID].
	versionID string
	// size is the reported content length, or [blob.SizeUnknown].
	size int64
	// mediaType is the media type of the object; see [Request.MediaType].
	mediaType string
}

func validate(req Request) error {
	if req.BucketName == "" {
		return errors.New("bucketName is required")
	}
	if req.ObjectKey == "" {
		return errors.New("objectKey is required")
	}
	return nil
}

// open sends a GetObject for req. An object whose reported size exceeds the limit is
// rejected before any of it is transferred; the caller closes the returned body.
func open(ctx context.Context, req Request, o *option) (*object, error) {
	if err := validate(req); err != nil {
		return nil, err
	}

	client, err := newClient(ctx, req, o)
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

	out, err := getObject(ctx, client, req, in)
	if err != nil {
		return nil, fmt.Errorf("error getting s3 object %s/%s: %w", req.BucketName, req.ObjectKey, err)
	}

	maxDownloadSize := o.maxDownloadSize()
	if maxDownloadSize > 0 && out.ContentLength != nil && *out.ContentLength > maxDownloadSize {
		_ = out.Body.Close()
		return nil, tooLarge(req, maxDownloadSize)
	}

	// A store that reports no length, or lies about it, is caught while streaming.
	body := out.Body
	if maxDownloadSize > 0 {
		body = &limitedBody{ReadCloser: out.Body, req: req, max: maxDownloadSize}
	}

	return &object{
		body:      body,
		versionID: aws.ToString(out.VersionId),
		size:      contentLength(out.ContentLength),
		mediaType: mediaType(req, out.ContentType),
	}, nil
}

// head sends a HeadObject for req and returns what it reports about the object; body is nil.
func head(ctx context.Context, req Request, o *option) (*object, error) {
	if err := validate(req); err != nil {
		return nil, err
	}

	client, err := newClient(ctx, req, o)
	if err != nil {
		return nil, err
	}

	in := &s3.HeadObjectInput{
		Bucket: new(req.BucketName),
		Key:    new(req.ObjectKey),
	}
	if req.Version != "" {
		in.VersionId = new(req.Version)
	}

	out, err := headObject(ctx, client, req, in)
	if err != nil {
		return nil, fmt.Errorf("error getting s3 object metadata %s/%s: %w", req.BucketName, req.ObjectKey, err)
	}
	return &object{
		versionID: aws.ToString(out.VersionId),
		size:      contentLength(out.ContentLength),
		mediaType: mediaType(req, out.ContentType),
	}, nil
}

func contentLength(length *int64) int64 {
	if length == nil {
		return blob.SizeUnknown
	}
	return *length
}

func mediaType(req Request, contentType *string) string {
	if req.MediaType != "" {
		return req.MediaType
	}
	if ct := aws.ToString(contentType); ct != "" {
		return ct
	}
	return defaultMediaType
}

func tooLarge(req Request, maxDownloadSize int64) error {
	return fmt.Errorf("s3 object %s/%s %w of %d bytes", req.BucketName, req.ObjectKey, errTooLarge, maxDownloadSize)
}

// limitedBody fails a read that takes the body past max bytes, rather than
// truncating it, so that an oversized object is never mistaken for a complete one.
type limitedBody struct {
	io.ReadCloser
	req  Request
	max  int64
	read int64
}

func (l *limitedBody) Read(p []byte) (int, error) {
	n, err := l.ReadCloser.Read(p)
	l.read += int64(n)
	if l.read > l.max {
		// Only the bytes up to the limit belong to the caller; a later call returns none.
		return max(0, n-int(l.read-l.max)), tooLarge(l.req, l.max)
	}
	return n, err
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
// Credentials that set any key or token are handed to the SDK as given, even a combination
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
// credentials are supplied, the AWS default chain is used. Anonymous access must
// be explicitly selected in the credentials.
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
	// A region in the specification has priority. If it is absent, the SDK resolves one
	// as every other AWS tool does, from AWS_REGION or the shared config profile.
	// [defaultRegion] applies only if that finds nothing either.
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
		if s3creds.Anonymous {
			loadOpts = append(loadOpts, config.WithCredentialsProvider(aws.AnonymousCredentials{}))
		} else if provider := staticCredentials(s3creds); provider != nil {
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
