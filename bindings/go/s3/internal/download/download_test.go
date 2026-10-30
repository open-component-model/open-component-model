package download

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

// s3Object is the canned response [fakeS3] serves for a GetObject request.
type s3Object struct {
	// body is the object content.
	body []byte
	// contentType is served as the Content-Type header. Empty sends none, which is
	// what an object stored without one looks like.
	contentType string
	// versionID is served as the x-amz-version-id header. Empty sends none, as a
	// store that does not report versions does.
	versionID string
	// errStatus and errCode serve an S3 error in place of the object.
	errStatus int
	errCode   string
	// chunked serves the body without a Content-Length, which is how a store that
	// does not know the object size up front answers.
	chunked bool
	// abortAfter drops the connection after that many bytes of body, simulating a
	// store failing mid-object. It implies chunked, since a length cannot be declared
	// for a body that is never finished.
	abortAfter *int
	// suppressBody serves the headers of an object but none of it, telling a caller that
	// decides from the reported length apart from one that reads first.
	suppressBody bool
	// declaredLength overrides the Content-Length header. Nil reports the true length
	// of body.
	//
	// It cannot understate the real length: the transport enforces the declared count,
	// so a store cannot hand out more than it announced.
	declaredLength *int64
}

// s3Request is one request a [fakeS3] answered.
type s3Request struct {
	method string
	// path is the request path, which is /bucket/key for path-style addressing.
	path string
	// versionID is the versionId query parameter, empty when none was sent.
	versionID string
	header    http.Header
}

// fakeS3 is an httptest server answering S3 GetObject requests with a canned object,
// so that the download is exercised over the wire it actually uses: the request is
// signed, addressed and parsed by the real AWS SDK, and only the store is fake. It
// authenticates nothing; what is under test is the download, not S3's auth.
type fakeS3 struct {
	*httptest.Server

	object s3Object

	mu       sync.Mutex
	requests []s3Request
}

// newFakeS3 starts a server serving object, and stops it when the test ends.
func newFakeS3(t *testing.T, object s3Object) *fakeS3 {
	t.Helper()

	f := &fakeS3{object: object}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)

	return f
}

// recorded returns the requests the server answered, in the order it answered them.
func (f *fakeS3) recorded() []s3Request {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]s3Request(nil), f.requests...)
}

func (f *fakeS3) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, s3Request{
		method:    r.Method,
		path:      r.URL.Path,
		versionID: r.URL.Query().Get("versionId"),
		header:    r.Header.Clone(),
	})
	f.mu.Unlock()

	if f.object.errStatus != 0 {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(f.object.errStatus)
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code></Error>`, f.object.errCode)
		return
	}

	if f.object.contentType != "" {
		w.Header().Set("Content-Type", f.object.contentType)
	} else {
		// A nil entry suppresses the type the server would otherwise sniff from the
		// body, so that an object stored without a content type stays without one.
		w.Header()["Content-Type"] = nil
	}
	if f.object.versionID != "" {
		w.Header().Set("X-Amz-Version-Id", f.object.versionID)
	}

	// Real S3 reports a checksum, and the SDK warns about every response carrying
	// none. A body that is cut short or never sent cannot be summed.
	if !f.object.suppressBody && f.object.abortAfter == nil {
		w.Header().Set("X-Amz-Checksum-Crc32", checksumCRC32(f.object.body))
	}

	length := int64(len(f.object.body))
	if f.object.declaredLength != nil {
		length = *f.object.declaredLength
	}

	// Flushing before the body is written is what makes the response chunked: the headers
	// go out before the length is known. An unflushed one has its Content-Length filled in.
	if f.object.chunked || f.object.abortAfter != nil {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusOK)
	}

	if f.object.suppressBody {
		return
	}

	body := f.object.body
	if n := f.object.abortAfter; n != nil {
		if *n < len(body) {
			body = body[:*n]
		}
		_, _ = w.Write(body)
		w.(http.Flusher).Flush()
		// The connection is dropped with the object half-delivered. ErrAbortHandler is
		// the panic the server swallows silently rather than logging as a failure.
		panic(http.ErrAbortHandler)
	}

	_, _ = w.Write(body)
}

// checksumCRC32 is the CRC32 of body in the base64 form S3 reports it in.
func checksumCRC32(body []byte) string {
	sum := make([]byte, 4)
	binary.BigEndian.PutUint32(sum, crc32.ChecksumIEEE(body))

	return base64.StdEncoding.EncodeToString(sum)
}

// fakeCredentials are static credentials for [fakeS3]. Their value is irrelevant, since
// the fake verifies no signature, but supplying them keeps the AWS default credential
// chain and its instance-metadata lookups out of the tests.
func fakeCredentials() *credv1.S3Credentials {
	return &credv1.S3Credentials{
		Type:            credv1.S3CredentialsVersionedType,
		AccessKeyID:     "test-access-key",
		SecretAccessKey: "test-secret-key",
	}
}

// at addresses req to srv. The fake serves no bucket subdomains, so the request is
// addressed path-style.
func at(srv *fakeS3, req Request) Request {
	req.Endpoint = srv.URL
	req.UsePathStyle = true
	return req
}

// streamFrom streams req from srv with [fakeCredentials] and returns what was written.
func streamFrom(t *testing.T, srv *fakeS3, req Request, opts ...Option) (*Result, []byte, error) {
	t.Helper()

	var buf bytes.Buffer
	res, err := Stream(t.Context(), at(srv, req), &buf, append([]Option{WithCredentials(fakeCredentials())}, opts...)...)
	return res, buf.Bytes(), err
}

// blobFrom returns req at srv as a lazy blob with [fakeCredentials].
func blobFrom(t *testing.T, srv *fakeS3, req Request, opts ...Option) *Blob {
	t.Helper()

	return NewBlob(t.Context(), at(srv, req), append([]Option{WithCredentials(fakeCredentials())}, opts...)...)
}

func readBlob(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	r := require.New(t)
	rc, err := b.ReadCloser()
	r.NoError(err)
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	r.NoError(err)
	return data
}

// methods returns the HTTP methods of requests, in order.
func methods(requests []s3Request) []string {
	out := make([]string, 0, len(requests))
	for _, req := range requests {
		out = append(out, req.method)
	}
	return out
}

func TestDownload_RequiredFields(t *testing.T) {
	tests := []struct {
		name    string
		req     Request
		wantErr string
	}{
		{
			name:    "missing bucket name",
			req:     Request{ObjectKey: "key"},
			wantErr: "bucketName is required",
		},
		{
			name:    "missing object key",
			req:     Request{BucketName: "bucket"},
			wantErr: "objectKey is required",
		},
		{
			name:    "missing both",
			req:     Request{},
			wantErr: "bucketName is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			srv := newFakeS3(t, s3Object{body: []byte("unused")})

			_, _, err := streamFrom(t, srv, tt.req)
			r.ErrorContains(err, tt.wantErr)

			b := blobFrom(t, srv, tt.req)
			_, err = b.ReadCloser()
			r.ErrorContains(err, tt.wantErr)
			r.Equal(blob.SizeUnknown, b.Size())

			r.Empty(srv.recorded(), "validation must happen before the object is requested")
		})
	}
}

func TestStream_WritesObject(t *testing.T) {
	r := require.New(t)
	content := []byte("hello from s3")
	srv := newFakeS3(t, s3Object{body: content, contentType: "text/plain", versionID: "v-1"})

	res, written, err := streamFrom(t, srv, Request{BucketName: "my-bucket", ObjectKey: "path/blob.txt"},
		WithDigestAlgorithm(digest.SHA256))
	r.NoError(err)
	r.Equal(content, written)
	r.Equal(&Result{
		VersionID: "v-1",
		Digest:    digest.SHA256.FromBytes(content),
		Size:      int64(len(content)),
		MediaType: "text/plain",
	}, res)

	requests := srv.recorded()
	r.Len(requests, 1)
	r.Equal(http.MethodGet, requests[0].method)
	r.Equal("/my-bucket/path/blob.txt", requests[0].path)
	r.Empty(requests[0].versionID, "no version pinned means the latest object")
	r.NotEmpty(requests[0].header.Get("Authorization"), "the request must be signed")
}

func TestStream_DigestOnlyOnRequest(t *testing.T) {
	r := require.New(t)
	srv := newFakeS3(t, s3Object{body: []byte("content")})

	res, _, err := streamFrom(t, srv, Request{BucketName: "b", ObjectKey: "k"})
	r.NoError(err)
	r.Empty(res.Digest)
}

func TestDownload_PinnedVersionIsForwarded(t *testing.T) {
	r := require.New(t)
	srv := newFakeS3(t, s3Object{body: []byte("versioned")})
	req := Request{BucketName: "b", ObjectKey: "k", Version: "v-1"}

	_, _, err := streamFrom(t, srv, req)
	r.NoError(err)
	readBlob(t, blobFrom(t, srv, req))

	requests := srv.recorded()
	r.Len(requests, 2)
	for _, request := range requests {
		r.Equal("v-1", request.versionID)
	}
}

func TestStream_ResolvedVersionIsReturned(t *testing.T) {
	tests := []struct {
		name      string
		versionID string
		want      string
	}{
		{
			name:      "versioned bucket reports the object version",
			versionID: "3sL4kqtJlcpXroDTDmJ+rmSpXd3dIbrHY+MTRCxf3vjVBH40Nr8X8gdRQBpUMLUo",
			want:      "3sL4kqtJlcpXroDTDmJ+rmSpXd3dIbrHY+MTRCxf3vjVBH40Nr8X8gdRQBpUMLUo",
		},
		{
			name:      "unversioned object reports the null placeholder",
			versionID: UnversionedVersionID,
			want:      UnversionedVersionID,
		},
		{
			name:      "store reporting no version at all yields an empty version",
			versionID: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			srv := newFakeS3(t, s3Object{body: []byte("content"), versionID: tt.versionID})

			res, _, err := streamFrom(t, srv, Request{BucketName: "b", ObjectKey: "k"})
			r.NoError(err)
			r.Equal(tt.want, res.VersionID)
		})
	}
}

func TestDownload_MediaType(t *testing.T) {
	tests := []struct {
		name        string
		mediaType   string
		contentType string
		want        string
	}{
		{
			name:        "request media type wins over the object content type",
			mediaType:   "application/custom",
			contentType: "text/plain",
			want:        "application/custom",
		},
		{
			name:        "object content type is used when the request sets none",
			contentType: "text/plain",
			want:        "text/plain",
		},
		{
			name:      "falls back to octet-stream when neither is set",
			mediaType: "",
			want:      "application/octet-stream",
		},
		{
			name:      "request media type is used when the object has no content type",
			mediaType: "application/yaml",
			want:      "application/yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			srv := newFakeS3(t, s3Object{body: []byte("content"), contentType: tt.contentType})
			req := Request{BucketName: "b", ObjectKey: "k", MediaType: tt.mediaType}

			res, _, err := streamFrom(t, srv, req)
			r.NoError(err)
			r.Equal(tt.want, res.MediaType)

			for name, b := range map[string]*Blob{"before a read": blobFrom(t, srv, req), "after a read": blobFrom(t, srv, req)} {
				if name == "after a read" {
					readBlob(t, b)
				}
				got, known := b.MediaType()
				r.True(known, name)
				r.Equal(tt.want, got, name)
			}
		})
	}
}

func TestDownload_GetObjectErrorIsWrapped(t *testing.T) {
	r := require.New(t)
	srv := newFakeS3(t, s3Object{errStatus: http.StatusForbidden, errCode: "AccessDenied"})
	req := Request{BucketName: "my-bucket", ObjectKey: "my-key"}

	_, _, err := streamFrom(t, srv, req)
	r.ErrorContains(err, "error getting s3 object my-bucket/my-key")
	r.ErrorContains(err, "AccessDenied", "the underlying cause must be preserved")

	_, err = blobFrom(t, srv, req).ReadCloser()
	r.ErrorContains(err, "error getting s3 object my-bucket/my-key")
	r.ErrorContains(err, "AccessDenied")
}

func TestStream_BodyReadErrorIsWrapped(t *testing.T) {
	r := require.New(t)
	srv := newFakeS3(t, s3Object{body: []byte("0123456789"), abortAfter: new(5)})

	_, _, err := streamFrom(t, srv, Request{BucketName: "my-bucket", ObjectKey: "my-key"})
	r.ErrorContains(err, "error streaming s3 object my-bucket/my-key")
}

func TestStream_WriterErrorIsWrapped(t *testing.T) {
	r := require.New(t)
	srv := newFakeS3(t, s3Object{body: []byte("content")})
	failed := errors.New("disk full")

	_, err := Stream(t.Context(), at(srv, Request{BucketName: "b", ObjectKey: "k"}), failingWriter{failed},
		WithCredentials(fakeCredentials()))
	r.ErrorIs(err, failed)
	r.ErrorContains(err, "error streaming s3 object b/k")
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

func TestDownload_MaxDownloadSize(t *testing.T) {
	content := []byte("0123456789")

	tests := []struct {
		name    string
		maxSize int64
		unset   bool
		object  s3Object
		wantErr bool
	}{
		{name: "body below the limit", maxSize: 100},
		{name: "body exactly at the limit", maxSize: 10},
		{name: "body one byte over the limit", maxSize: 9, wantErr: true},
		{name: "zero disables the limit", maxSize: 0},
		{name: "negative disables the limit", maxSize: -1},
		{name: "unset uses the default", unset: true},
		{name: "the largest limit does not overflow into a truncated read", maxSize: math.MaxInt64},
		{
			// The store announces the object and sends none of it, so a download consulting
			// the body first would fail on the truncated transfer rather than on the limit.
			name:    "oversized object is rejected from its reported length",
			maxSize: 5,
			object:  s3Object{declaredLength: new(int64(1 << 30)), suppressBody: true},
			wantErr: true,
		},
		{
			// A store cannot understate its length — the transport enforces the count it
			// declared — but one that declares none can hand out more than the limit.
			name:    "a store reporting no length is caught while streaming",
			maxSize: 5,
			object:  s3Object{chunked: true},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			object := tt.object
			if object.body == nil {
				object.body = content
			}
			srv := newFakeS3(t, object)
			req := Request{BucketName: "my-bucket", ObjectKey: "my-key"}

			var opts []Option
			if !tt.unset {
				opts = append(opts, WithMaxDownloadSize(tt.maxSize))
			}

			t.Run("stream", func(t *testing.T) {
				r := require.New(t)
				_, written, err := streamFrom(t, srv, req, opts...)
				if tt.wantErr {
					r.ErrorContains(err, "s3 object my-bucket/my-key exceeds maximum allowed size")
					return
				}
				r.NoError(err)
				r.Equal(content, written)
			})

			t.Run("blob", func(t *testing.T) {
				r := require.New(t)
				rc, err := blobFrom(t, srv, req, opts...).ReadCloser()
				if err == nil {
					defer func() { _ = rc.Close() }()
					var data []byte
					data, err = io.ReadAll(rc)
					if !tt.wantErr {
						r.NoError(err)
						r.Equal(content, data)
						return
					}
					r.LessOrEqual(len(data), int(tt.maxSize), "no byte past the limit may be handed out")
				}
				r.True(tt.wantErr, "unexpected error: %v", err)
				r.ErrorContains(err, "s3 object my-bucket/my-key exceeds maximum allowed size")
			})
		})
	}
}

func TestStream_EmptyObject(t *testing.T) {
	r := require.New(t)
	srv := newFakeS3(t, s3Object{body: []byte{}})

	res, written, err := streamFrom(t, srv, Request{BucketName: "b", ObjectKey: "k"})
	r.NoError(err)
	r.Empty(written)
	r.Zero(res.Size)
}

func TestDownload_CredentialConversionErrorIsWrapped(t *testing.T) {
	r := require.New(t)
	unknown := &runtime.Raw{Type: runtime.NewVersionedType("Unknown", "v1"), Data: []byte("{}")}
	req := Request{BucketName: "b", ObjectKey: "k"}

	_, err := Stream(t.Context(), req, io.Discard, WithCredentials(unknown))
	r.ErrorContains(err, "error converting s3 credentials")

	_, err = NewBlob(t.Context(), req, WithCredentials(unknown)).ReadCloser()
	r.ErrorContains(err, "error converting s3 credentials")
}

func TestBlob_EveryReadRequestsTheObject(t *testing.T) {
	r := require.New(t)
	content := []byte("read me twice")
	srv := newFakeS3(t, s3Object{body: content})

	b := blobFrom(t, srv, Request{BucketName: "b", ObjectKey: "k"})
	r.Empty(srv.recorded(), "creating the blob must not request the object")

	r.Equal(content, readBlob(t, b))
	r.Equal(content, readBlob(t, b))
	r.Equal([]string{http.MethodGet, http.MethodGet}, methods(srv.recorded()))
}

func TestBlob_PinsVersionOfFirstRead(t *testing.T) {
	tests := []struct {
		name      string
		versionID string
		want      string
	}{
		{name: "a reported version is pinned", versionID: "v-1", want: "v-1"},
		{name: "the unversioned placeholder pins nothing", versionID: UnversionedVersionID},
		{name: "a store reporting no version pins nothing"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			srv := newFakeS3(t, s3Object{body: []byte("content"), versionID: tt.versionID})
			b := blobFrom(t, srv, Request{BucketName: "b", ObjectKey: "k"})

			readBlob(t, b)
			readBlob(t, b)
			r.Equal(int64(len("content")), b.Size(), "the size is learned from the first read")

			requests := srv.recorded()
			r.Len(requests, 2, "size must not need a request of its own after a read")
			r.Empty(requests[0].versionID)
			r.Equal(tt.want, requests[1].versionID)
		})
	}
}

func TestBlob_DescribedByHeadBeforeRead(t *testing.T) {
	r := require.New(t)
	content := []byte("described")
	srv := newFakeS3(t, s3Object{body: content, contentType: "text/plain"})
	b := blobFrom(t, srv, Request{BucketName: "b", ObjectKey: "k"})

	r.Equal(int64(len(content)), b.Size())
	mediaType, known := b.MediaType()
	r.True(known)
	r.Equal("text/plain", mediaType)
	r.Equal([]string{http.MethodHead}, methods(srv.recorded()), "one HeadObject describes the object")

	r.Equal(content, readBlob(t, b))
	r.Equal([]string{http.MethodHead, http.MethodGet}, methods(srv.recorded()))
}

func TestBlob_PinsVersionDescribedByHead(t *testing.T) {
	r := require.New(t)
	srv := newFakeS3(t, s3Object{body: []byte("described"), versionID: "v-1"})
	b := blobFrom(t, srv, Request{BucketName: "b", ObjectKey: "k"})

	r.Equal(int64(len("described")), b.Size())
	readBlob(t, b)

	requests := srv.recorded()
	r.Equal([]string{http.MethodHead, http.MethodGet}, methods(requests))
	r.Equal("v-1", requests[1].versionID, "reads serve the version the size describes")
}

func TestBlob_FailedHeadLeavesItUndescribed(t *testing.T) {
	r := require.New(t)
	srv := newFakeS3(t, s3Object{errStatus: http.StatusForbidden, errCode: "AccessDenied"})
	b := blobFrom(t, srv, Request{BucketName: "b", ObjectKey: "k"})

	r.Equal(blob.SizeUnknown, b.Size())
	_, known := b.MediaType()
	r.False(known)
	r.Equal([]string{http.MethodHead}, methods(srv.recorded()), "a failed HeadObject is not repeated")

	withType := blobFrom(t, srv, Request{BucketName: "b", ObjectKey: "k", MediaType: "application/custom"})
	mediaType, known := withType.MediaType()
	r.True(known)
	r.Equal("application/custom", mediaType)
	r.Len(srv.recorded(), 1, "a media type of the request needs no request")
}

func TestBlob_ReadsAreBoundToContext(t *testing.T) {
	r := require.New(t)
	srv := newFakeS3(t, s3Object{body: []byte("content")})
	ctx, cancel := context.WithCancel(t.Context())
	b := NewBlob(ctx, at(srv, Request{BucketName: "b", ObjectKey: "k"}), WithCredentials(fakeCredentials()))
	cancel()

	_, err := b.ReadCloser()
	r.ErrorIs(err, context.Canceled)
}

func TestHTTPConfig(t *testing.T) {
	t.Run("nil config yields an empty config", func(t *testing.T) {
		got := httpConfig(nil)
		require.NotNil(t, got)
		require.NotNil(t, got.Retry)
		require.NotNil(t, got.Retry.MaxRetries)
		assert.Equal(t, disableRetry, *got.Retry.MaxRetries, "retrying is left to the SDK")
		assert.Nil(t, got.InsecureSkipVerify)
	})

	// Retrying is the SDK's job, so the transport handed to it must not retry as well;
	// otherwise the two attempt counts multiply.
	t.Run("the caller's retry config is disabled for the transport", func(t *testing.T) {
		callerRetries := 7
		cfg := &httpv1alpha1.Config{Retry: &httpv1alpha1.RetryConfig{MaxRetries: &callerRetries}}
		got := httpConfig(cfg)
		require.NotNil(t, got.Retry)
		require.NotNil(t, got.Retry.MaxRetries)
		assert.Equal(t, disableRetry, *got.Retry.MaxRetries)
	})

	// A per-host entry overrides the global policy, so leaving one on would reinstate
	// transport retries for exactly the host being downloaded from.
	t.Run("per-host retry is disabled too", func(t *testing.T) {
		hostRetries := 3
		got := httpConfig(&httpv1alpha1.Config{
			Hosts: map[string]*httpv1alpha1.HostConfig{
				"s3.amazonaws.com": {Retry: &httpv1alpha1.RetryConfig{MaxRetries: &hostRetries}},
			},
		})

		hc := got.Hosts["s3.amazonaws.com"]
		require.NotNil(t, hc)
		require.NotNil(t, hc.Retry)
		require.NotNil(t, hc.Retry.MaxRetries)
		assert.Equal(t, disableRetry, *hc.Retry.MaxRetries)
	})

	t.Run("a nil host entry is left alone", func(t *testing.T) {
		got := httpConfig(&httpv1alpha1.Config{
			Hosts: map[string]*httpv1alpha1.HostConfig{"s3.amazonaws.com": nil},
		})
		assert.Nil(t, got.Hosts["s3.amazonaws.com"])
	})

	t.Run("the caller's own insecureSkipVerify is preserved", func(t *testing.T) {
		skip := true
		got := httpConfig(&httpv1alpha1.Config{
			TLSConfig: httpv1alpha1.TLSConfig{InsecureSkipVerify: &skip},
			Hosts: map[string]*httpv1alpha1.HostConfig{
				"minio.internal": {TLSConfig: httpv1alpha1.TLSConfig{InsecureSkipVerify: &skip}},
			},
		})

		require.NotNil(t, got.InsecureSkipVerify)
		assert.True(t, *got.InsecureSkipVerify)
		hc := got.Hosts["minio.internal"]
		require.NotNil(t, hc)
		require.NotNil(t, hc.InsecureSkipVerify)
		assert.True(t, *hc.InsecureSkipVerify)
	})

	t.Run("verification stays enabled when the caller does not skip it", func(t *testing.T) {
		got := httpConfig(&httpv1alpha1.Config{})
		assert.Nil(t, got.InsecureSkipVerify, "TLS verification must not be touched unless the caller skips it")
	})

	t.Run("caller timeouts are preserved", func(t *testing.T) {
		timeout := httpv1alpha1.NewTimeout(42)
		got := httpConfig(&httpv1alpha1.Config{
			TimeoutConfig: httpv1alpha1.TimeoutConfig{Timeout: timeout},
		})
		require.NotNil(t, got.Timeout)
		assert.Equal(t, *timeout, *got.Timeout)
	})

	t.Run("the caller's config is not mutated", func(t *testing.T) {
		callerRetries := 7
		cfg := &httpv1alpha1.Config{
			Retry: &httpv1alpha1.RetryConfig{MaxRetries: &callerRetries},
		}
		got := httpConfig(cfg)

		assert.Equal(t, 7, *cfg.Retry.MaxRetries, "the caller keeps the retry count it configured")
		assert.Nil(t, cfg.InsecureSkipVerify, "the caller's TLS config must be left alone")
		assert.NotSame(t, cfg, got)
		assert.NotSame(t, cfg.Retry, got.Retry, "the retry config must be copied, not shared")
	})
}

// withoutAWSEnvironment hides the developer's or CI runner's own AWS setup from a
// test, so that a region the SDK resolves comes from the test alone. Empty values are
// treated as unset by the SDK, and the config files point nowhere so that a real
// ~/.aws/config cannot contribute a region either.
func withoutAWSEnvironment(t *testing.T) {
	t.Helper()

	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "AWS_") {
			t.Setenv(key, "")
		}
	}
	missing := filepath.Join(t.TempDir(), "absent")
	for k, v := range map[string]string{
		"AWS_EC2_METADATA_DISABLED":   "true",
		"AWS_REGION":                  "",
		"AWS_DEFAULT_REGION":          "",
		"AWS_PROFILE":                 "",
		"AWS_CONFIG_FILE":             missing,
		"AWS_SHARED_CREDENTIALS_FILE": missing,
	} {
		t.Setenv(k, v)
	}
}

func TestNewClient(t *testing.T) {
	withoutAWSEnvironment(t)
	ctx := t.Context()

	t.Run("the request addresses the client", func(t *testing.T) {
		for _, tt := range []struct {
			name          string
			req           Request
			env           map[string]string
			wantRegion    string
			wantEndpoint  string
			wantPathStyle bool
		}{
			{
				name:       "region defaults when neither the request nor the environment names one",
				req:        Request{BucketName: "b", ObjectKey: "k"},
				wantRegion: defaultRegion,
			},
			{
				name:       "explicit region is used",
				req:        Request{Region: "eu-central-1"},
				wantRegion: "eu-central-1",
			},
			{
				name:       "the environment supplies the region when the request omits it",
				req:        Request{BucketName: "b", ObjectKey: "k"},
				env:        map[string]string{"AWS_REGION": "ap-southeast-2"},
				wantRegion: "ap-southeast-2",
			},
			{
				name:       "the request's region wins over the environment",
				req:        Request{Region: "eu-central-1"},
				env:        map[string]string{"AWS_REGION": "ap-southeast-2"},
				wantRegion: "eu-central-1",
			},
			{
				name:          "custom endpoint and path style are applied",
				req:           Request{Endpoint: "https://minio.internal:9000", UsePathStyle: true},
				wantRegion:    defaultRegion,
				wantEndpoint:  "https://minio.internal:9000",
				wantPathStyle: true,
			},
			{
				// AWS is targeted when no endpoint is given.
				name:       "no endpoint leaves the base endpoint unset",
				req:        Request{Region: "us-west-2"},
				wantRegion: "us-west-2",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				withoutAWSEnvironment(t)
				for k, v := range tt.env {
					t.Setenv(k, v)
				}

				client, err := newClient(ctx, tt.req, &option{})
				require.NoError(t, err)
				assert.Equal(t, tt.wantRegion, client.Options().Region)
				assert.Equal(t, tt.wantEndpoint, aws.ToString(client.Options().BaseEndpoint))
				assert.Equal(t, tt.wantPathStyle, client.Options().UsePathStyle)
			})
		}
	})

	t.Run("static credentials are applied", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			sessionToken string
		}{
			{name: "with a session token", sessionToken: "session"},
			{name: "without a session token"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				client, err := newClient(ctx, Request{Region: "us-east-1"}, &option{Credentials: &credv1.S3Credentials{
					Type:            credv1.S3CredentialsVersionedType,
					AccessKeyID:     "AKIA",
					SecretAccessKey: "secret",
					SessionToken:    tt.sessionToken,
				}})
				require.NoError(t, err)

				creds, err := client.Options().Credentials.Retrieve(ctx)
				require.NoError(t, err)
				assert.Equal(t, "AKIA", creds.AccessKeyID)
				assert.Equal(t, "secret", creds.SecretAccessKey)
				assert.Equal(t, tt.sessionToken, creds.SessionToken)
			})
		}
	})

	t.Run("explicit anonymous credentials disable the signing provider", func(t *testing.T) {
		r := require.New(t)
		t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
		client, err := newClient(t.Context(), Request{}, &option{Credentials: &credv1.S3Credentials{
			Type: credv1.S3CredentialsVersionedType, Anonymous: true,
		}})
		r.NoError(err)
		// The S3 SDK normalizes the anonymous sentinel to a nil signing provider.
		r.Nil(client.Options().Credentials)
	})

	t.Run("empty credentials fall through to the default chain", func(t *testing.T) {
		t.Setenv("AWS_ACCESS_KEY_ID", "env-key")
		t.Setenv("AWS_SECRET_ACCESS_KEY", "env-secret")
		client, err := newClient(ctx, Request{Region: "us-east-1"}, &option{Credentials: &credv1.S3Credentials{
			Type: credv1.S3CredentialsVersionedType,
		}})
		require.NoError(t, err)
		creds, err := client.Options().Credentials.Retrieve(ctx)
		require.NoError(t, err)
		require.Equal(t, "env-key", creds.AccessKeyID)
	})

	// What matters is that a half-filled credential reaches the SDK at all: dropping it
	// would send the request under whatever identity the default chain resolves.
	t.Run("partial credentials reach the SDK, which rejects them", func(t *testing.T) {
		for _, tt := range []struct {
			name  string
			creds credv1.S3Credentials
		}{
			{name: "secret access key without an access key id", creds: credv1.S3Credentials{SecretAccessKey: "secret"}},
			{name: "access key id without a secret access key", creds: credv1.S3Credentials{AccessKeyID: "AKIA"}},
			{name: "session token alone", creds: credv1.S3Credentials{SessionToken: "session"}},
			{
				name:  "access key id and session token without a secret",
				creds: credv1.S3Credentials{AccessKeyID: "AKIA", SessionToken: "session"},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				creds := tt.creds
				creds.Type = credv1.S3CredentialsVersionedType

				client, err := newClient(ctx, Request{Region: "us-east-1"}, &option{Credentials: &creds})
				require.NoError(t, err, "building the client does not validate the credential")

				_, err = client.Options().Credentials.Retrieve(ctx)
				var empty *credentials.StaticCredentialsEmptyError
				require.ErrorAs(t, err, &empty,
					"the configured credential must be the one that fails, not a fallback from the default chain")
			})
		}
	})

	t.Run("an HTTP client is always installed", func(t *testing.T) {
		client, err := newClient(ctx, Request{}, &option{})
		require.NoError(t, err)
		assert.NotNil(t, client.Options().HTTPClient, "the shared ocm HTTP client must back the s3 client")
	})

	t.Run("a supplied HTTP client is used as-is", func(t *testing.T) {
		injected := &http.Client{}
		client, err := newClient(ctx, Request{Region: "us-east-1"}, &option{HTTPClient: injected})
		require.NoError(t, err)
		assert.Same(t, injected, client.Options().HTTPClient)
	})

	t.Run("a supplied HTTP client wins over the HTTP config", func(t *testing.T) {
		injected := &http.Client{}
		client, err := newClient(ctx, Request{}, &option{
			HTTPClient: injected,
			HTTPConfig: &httpv1alpha1.Config{TimeoutConfig: httpv1alpha1.TimeoutConfig{Timeout: httpv1alpha1.NewTimeout(42)}},
		})
		require.NoError(t, err)
		assert.Same(t, injected, client.Options().HTTPClient, "the caller's client must not be rebuilt from config")
	})

	// Retrying happens in the SDK alone, driven by the ocm retry configuration, so the
	// configured count is not multiplied by a second layer of transport retries.
	t.Run("the configured retry drives the SDK", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			maxRetries *int
			want       int
		}{
			{name: "retries become attempts including the initial request", maxRetries: new(3), want: 4},
			{name: "disabled retry is a single attempt", maxRetries: new(disableRetry), want: 1},
			{name: "infinite retry has no attempt ceiling", maxRetries: new(0), want: math.MaxInt},
		} {
			t.Run(tt.name, func(t *testing.T) {
				client, err := newClient(ctx, Request{}, &option{
					HTTPConfig: &httpv1alpha1.Config{Retry: &httpv1alpha1.RetryConfig{MaxRetries: tt.maxRetries}},
				})
				require.NoError(t, err)
				assert.Equal(t, tt.want, client.Options().RetryMaxAttempts)
			})
		}
	})

	// A per-host retry entry replaces the global one; an absent entry inherits it.
	t.Run("a per-host retry overrides the global one", func(t *testing.T) {
		retryCfg := func(maxRetries int) *httpv1alpha1.RetryConfig {
			return &httpv1alpha1.RetryConfig{MaxRetries: &maxRetries}
		}

		for _, tt := range []struct {
			name     string
			endpoint string
			cfg      *httpv1alpha1.Config
			want     int
		}{
			{
				name:     "the host:port entry applies",
				endpoint: "https://minio.internal:9000",
				cfg: &httpv1alpha1.Config{
					Retry: retryCfg(3),
					Hosts: map[string]*httpv1alpha1.HostConfig{
						"minio.internal:9000": {Retry: retryCfg(10)},
					},
				},
				want: 11,
			},
			{
				name:     "a bare hostname entry applies to an endpoint with a port",
				endpoint: "https://minio.internal:9000",
				cfg: &httpv1alpha1.Config{
					Retry: retryCfg(3),
					Hosts: map[string]*httpv1alpha1.HostConfig{
						"minio.internal": {Retry: retryCfg(10)},
					},
				},
				want: 11,
			},
			{
				name:     "the host:port entry wins over the bare hostname",
				endpoint: "https://minio.internal:9000",
				cfg: &httpv1alpha1.Config{
					Retry: retryCfg(3),
					Hosts: map[string]*httpv1alpha1.HostConfig{
						"minio.internal:9000": {Retry: retryCfg(10)},
						"minio.internal":      {Retry: retryCfg(7)},
					},
				},
				want: 11,
			},
			{
				name:     "a disabled per-host retry is a single attempt",
				endpoint: "https://minio.internal:9000",
				cfg: &httpv1alpha1.Config{
					Retry: retryCfg(3),
					Hosts: map[string]*httpv1alpha1.HostConfig{
						"minio.internal:9000": {Retry: retryCfg(disableRetry)},
					},
				},
				want: 1,
			},
			{
				name:     "an entry for another host does not apply",
				endpoint: "https://minio.internal:9000",
				cfg: &httpv1alpha1.Config{
					Retry: retryCfg(3),
					Hosts: map[string]*httpv1alpha1.HostConfig{
						"ceph.internal": {Retry: retryCfg(10)},
					},
				},
				want: 4,
			},
			{
				name:     "an entry without a retry section inherits the global one",
				endpoint: "https://minio.internal:9000",
				cfg: &httpv1alpha1.Config{
					Retry: retryCfg(3),
					Hosts: map[string]*httpv1alpha1.HostConfig{
						"minio.internal:9000": {},
					},
				},
				want: 4,
			},
			{
				name:     "a nil entry inherits the global one",
				endpoint: "https://minio.internal:9000",
				cfg: &httpv1alpha1.Config{
					Retry: retryCfg(3),
					Hosts: map[string]*httpv1alpha1.HostConfig{"minio.internal:9000": nil},
				},
				want: 4,
			},
			{
				name:     "a per-host entry applies without a global retry",
				endpoint: "https://minio.internal:9000",
				cfg: &httpv1alpha1.Config{
					Hosts: map[string]*httpv1alpha1.HostConfig{
						"minio.internal:9000": {Retry: retryCfg(10)},
					},
				},
				want: 11,
			},
			// AWS resolves the request host in the SDK's endpoint resolver, so there is
			// nothing to match a per-host entry against and the global setting stands.
			{
				name:     "no endpoint keeps the global setting",
				endpoint: "",
				cfg: &httpv1alpha1.Config{
					Retry: retryCfg(3),
					Hosts: map[string]*httpv1alpha1.HostConfig{
						"minio.internal:9000": {Retry: retryCfg(10)},
					},
				},
				want: 4,
			},
			{
				name:     "an endpoint with no parsable host keeps the global setting",
				endpoint: "minio.internal:9000",
				cfg: &httpv1alpha1.Config{
					Retry: retryCfg(3),
					Hosts: map[string]*httpv1alpha1.HostConfig{
						"minio.internal:9000": {Retry: retryCfg(10)},
					},
				},
				want: 4,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				client, err := newClient(ctx, Request{Endpoint: tt.endpoint}, &option{HTTPConfig: tt.cfg})
				require.NoError(t, err)
				assert.Equal(t, tt.want, client.Options().RetryMaxAttempts)
			})
		}
	})

	t.Run("an unconfigured retry leaves the SDK on its own default", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			cfg  *httpv1alpha1.Config
		}{
			{name: "no http config at all", cfg: nil},
			{name: "config without a retry section", cfg: &httpv1alpha1.Config{}},
			{name: "retry section without a count", cfg: &httpv1alpha1.Config{Retry: &httpv1alpha1.RetryConfig{}}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				client, err := newClient(ctx, Request{}, &option{HTTPConfig: tt.cfg})
				require.NoError(t, err)
				assert.Zero(t, client.Options().RetryMaxAttempts, "an unset value lets the SDK apply its default")
				assert.Equal(t, retry.DefaultMaxAttempts, client.Options().Retryer.MaxAttempts())
			})
		}
	})
}
