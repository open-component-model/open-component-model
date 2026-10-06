package download

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"ocm.software/open-component-model/bindings/go/blob"
)

// Stream is a lazy, replayable blob that streams an S3 object body on each
// [Stream.ReadCloser] call by re-issuing the GetObject request. No temporary file
// is created and no bytes are buffered: the body is read straight from the store.
//
// Replayability is provided by re-issuing the request, so the blob honours the
// [blob.ReadOnlyBlob] contract that every ReadCloser starts from the beginning.
// The common by-value transfer path reads the blob exactly once, so a single
// request is issued.
//
// The context captured at construction time governs every request the blob issues;
// a lazy blob has no other place to carry it.
type Stream struct {
	ctx     context.Context
	req     Request
	o       *option
	maxSize int64

	mu        sync.Mutex
	client    *s3.Client
	mediaType string
}

var (
	_ blob.ReadOnlyBlob   = (*Stream)(nil)
	_ blob.MediaTypeAware = (*Stream)(nil)
)

// Idempotent reports that the source request can be safely re-issued. The S3
// source is always a GetObject, so repeating it is safe.
func (s *Stream) Idempotent() bool {
	return true
}

// NewStream builds a lazy streaming blob for the S3 object described by req. The
// S3 client is created on the first read and reused on subsequent reads. Options
// mirror those of [Download] (credentials, max size, HTTP client/config);
// [WithTempDir] has no effect because nothing is written to disk.
func NewStream(ctx context.Context, req Request, opts ...Option) *Stream {
	o := &option{}
	for _, opt := range opts {
		opt(o)
	}
	maxSize := DefaultMaxDownloadSize
	if o.MaxDownloadSize != nil {
		maxSize = *o.MaxDownloadSize
	}
	return &Stream{
		ctx:       ctx,
		req:       req,
		o:         o,
		maxSize:   maxSize,
		mediaType: req.MediaType,
	}
}

// MediaType reports the configured media type, or the one captured from the first
// response when the request did not pin one. It is unknown until the first read
// when neither is available.
func (s *Stream) MediaType() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mediaType == "" {
		return "", false
	}
	return s.mediaType, true
}

// ReadCloser issues a GetObject request and returns a reader over the object body.
// Each call starts a fresh request, so the blob is replayable.
func (s *Stream) ReadCloser() (io.ReadCloser, error) {
	if s.req.BucketName == "" {
		return nil, fmt.Errorf("bucketName is required")
	}
	if s.req.ObjectKey == "" {
		return nil, fmt.Errorf("objectKey is required")
	}

	getter, err := s.getClient()
	if err != nil {
		return nil, err
	}

	in := &s3.GetObjectInput{
		Bucket: new(s.req.BucketName),
		Key:    new(s.req.ObjectKey),
	}
	if s.req.Version != "" {
		in.VersionId = new(s.req.Version)
	}

	out, err := getObject(s.ctx, getter, s.req, in)
	if err != nil {
		return nil, fmt.Errorf("error getting s3 object %s/%s: %w", s.req.BucketName, s.req.ObjectKey, err)
	}

	// Reject an oversized object before any of it is read; S3 reports the size up front.
	if s.maxSize > 0 && out.ContentLength != nil && *out.ContentLength > s.maxSize {
		_ = out.Body.Close()
		return nil, fmt.Errorf("s3 object %s/%s exceeds maximum allowed size of %d bytes", s.req.BucketName, s.req.ObjectKey, s.maxSize)
	}

	s.mu.Lock()
	if s.mediaType == "" {
		if ct := aws.ToString(out.ContentType); ct != "" {
			s.mediaType = ct
		}
	}
	s.mu.Unlock()

	return &streamReader{
		body:       out.Body,
		bucketName: s.req.BucketName,
		objectKey:  s.req.ObjectKey,
		maxSize:    s.maxSize,
	}, nil
}

// getClient builds the S3 client on first use and reuses it afterwards.
func (s *Stream) getClient() (*s3.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	client, err := newClient(s.ctx, s.req, s.o)
	if err != nil {
		return nil, err
	}
	s.client = client
	return client, nil
}

// streamReader reads a single S3 object body and enforces the size limit while
// streaming. It owns the response body and closes it on Close.
type streamReader struct {
	body       io.ReadCloser
	bucketName string
	objectKey  string
	maxSize    int64
	read       int64
}

func (r *streamReader) Read(p []byte) (int, error) {
	// Bound the read so a store that lies about ContentLength cannot stream past the
	// limit; the extra byte separates reaching the limit from exceeding it.
	if r.maxSize > 0 {
		remaining := r.maxSize - r.read + 1
		if remaining <= 0 {
			return 0, fmt.Errorf("s3 object %s/%s exceeds maximum allowed size of %d bytes", r.bucketName, r.objectKey, r.maxSize)
		}
		if int64(len(p)) > remaining {
			p = p[:remaining]
		}
	}
	n, err := r.body.Read(p)
	if n > 0 {
		r.read += int64(n)
	}
	if r.maxSize > 0 && r.read > r.maxSize {
		return n, fmt.Errorf("s3 object %s/%s exceeds maximum allowed size of %d bytes", r.bucketName, r.objectKey, r.maxSize)
	}
	return n, err
}

func (r *streamReader) Close() error {
	return r.body.Close()
}
