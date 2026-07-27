package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

// fakeGetter is a stand-in S3 client returning canned object content. It records
// the input it was called with, so download tests need no network or real bucket.
type fakeGetter struct {
	body        []byte
	contentType string
	// contentLength overrides the reported object size. When nil, len(body) is used.
	contentLength *int64
	// bodyReader overrides body, so tests can inject a reader that fails mid-stream.
	bodyReader io.ReadCloser
	// versionID is the object version S3 reports for the read. Empty reports none.
	versionID string
	// err is returned instead of an output when set.
	err error

	gotInput *s3.GetObjectInput
	// closed records whether the returned body was closed.
	closed bool
}

func (f *fakeGetter) GetObject(_ context.Context, in *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	f.gotInput = in
	if f.err != nil {
		return nil, f.err
	}

	body := f.bodyReader
	if body == nil {
		body = io.NopCloser(bytes.NewReader(f.body))
	}

	length := int64(len(f.body))
	if f.contentLength != nil {
		length = *f.contentLength
	}

	out := &s3.GetObjectOutput{
		Body:          &trackedBody{ReadCloser: body, closed: &f.closed},
		ContentLength: aws.Int64(length),
	}
	if f.contentType != "" {
		out.ContentType = aws.String(f.contentType)
	}
	if f.versionID != "" {
		out.VersionId = aws.String(f.versionID)
	}
	return out, nil
}

type trackedBody struct {
	io.ReadCloser
	closed *bool
}

func (t *trackedBody) Close() error {
	*t.closed = true
	return t.ReadCloser.Close()
}

// errReader fails after yielding prefix, simulating a connection dropping mid-object.
type errReader struct {
	prefix []byte
	off    int
	err    error
}

func (e *errReader) Read(p []byte) (int, error) {
	if e.off < len(e.prefix) {
		n := copy(p, e.prefix[e.off:])
		e.off += n
		return n, nil
	}
	return 0, e.err
}

func (e *errReader) Close() error { return nil }

func readBlob(t *testing.T, b blob.ReadOnlyBlob) []byte {
	t.Helper()
	rc, err := b.ReadCloser()
	require.NoError(t, err)
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	return data
}

func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
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
			// A client is injected so a validation miss would surface as a call
			// attempt rather than passing silently.
			fake := &fakeGetter{body: []byte("unused")}
			_, err := Download(t.Context(), tt.req, WithClient(fake), WithTempDir(t.TempDir()))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Nil(t, fake.gotInput, "validation must happen before GetObject is called")
		})
	}
}

func TestDownload_ReturnsObjectBody(t *testing.T) {
	content := []byte("hello from s3")
	fake := &fakeGetter{body: content, contentType: "text/plain"}

	res, err := Download(t.Context(), Request{BucketName: "my-bucket", ObjectKey: "path/blob.txt"},
		WithClient(fake), WithTempDir(t.TempDir()))
	require.NoError(t, err)

	assert.Equal(t, content, readBlob(t, res.Blob))
	assert.True(t, fake.closed, "the object body must be closed")

	assert.Equal(t, "my-bucket", aws.ToString(fake.gotInput.Bucket))
	assert.Equal(t, "path/blob.txt", aws.ToString(fake.gotInput.Key))
	assert.Nil(t, fake.gotInput.VersionId, "no version pinned means the latest object")
}

// TestDownload_BlobIsReReadable guards the blob.ReadOnlyBlob contract: ReadCloser
// may be called repeatedly, each call reading from the start.
func TestDownload_BlobIsReReadable(t *testing.T) {
	content := []byte("read me twice")
	res, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: content}), WithTempDir(t.TempDir()))
	require.NoError(t, err)

	assert.Equal(t, content, readBlob(t, res.Blob))
	assert.Equal(t, content, readBlob(t, res.Blob))
}

func TestDownload_BlobReportsSize(t *testing.T) {
	content := []byte("sized content")
	res, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: content}), WithTempDir(t.TempDir()))
	require.NoError(t, err)

	sizeAware, ok := res.Blob.(blob.SizeAware)
	require.True(t, ok, "the returned blob must report its size")
	assert.Equal(t, int64(len(content)), sizeAware.Size())
}

func TestDownload_PinnedVersionIsForwarded(t *testing.T) {
	fake := &fakeGetter{body: []byte("versioned")}

	_, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k", Version: "v-1"},
		WithClient(fake), WithTempDir(t.TempDir()))
	require.NoError(t, err)

	assert.Equal(t, "v-1", aws.ToString(fake.gotInput.VersionId))
}

// TestDownload_ResolvedVersionIsReturned covers the version S3 reports for the read.
// The repository pins the access spec with it, so it has to survive the download.
func TestDownload_ResolvedVersionIsReturned(t *testing.T) {
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
			res, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
				WithClient(&fakeGetter{body: []byte("content"), versionID: tt.versionID}),
				WithTempDir(t.TempDir()))
			require.NoError(t, err)
			assert.Equal(t, tt.want, res.VersionID)
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
			res, err := Download(t.Context(),
				Request{BucketName: "b", ObjectKey: "k", MediaType: tt.mediaType},
				WithClient(&fakeGetter{body: []byte("content"), contentType: tt.contentType}),
				WithTempDir(t.TempDir()))
			require.NoError(t, err)

			mediaTypeAware, ok := res.Blob.(blob.MediaTypeAware)
			require.True(t, ok, "the returned blob must report its media type")
			got, known := mediaTypeAware.MediaType()
			assert.True(t, known)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDownload_GetObjectErrorIsWrapped(t *testing.T) {
	fake := &fakeGetter{err: fmt.Errorf("access denied")}

	_, err := Download(t.Context(), Request{BucketName: "my-bucket", ObjectKey: "my-key"},
		WithClient(fake), WithTempDir(t.TempDir()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error getting s3 object my-bucket/my-key")
	assert.Contains(t, err.Error(), "access denied", "the underlying cause must be preserved")
}

func TestDownload_BodyReadErrorIsWrappedAndFileRemoved(t *testing.T) {
	tempDir := t.TempDir()
	fake := &fakeGetter{
		body:       []byte("0123456789"),
		bodyReader: &errReader{prefix: []byte("01234"), err: fmt.Errorf("connection reset")},
	}

	_, err := Download(t.Context(), Request{BucketName: "my-bucket", ObjectKey: "my-key"},
		WithClient(fake), WithTempDir(tempDir))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error writing s3 object my-bucket/my-key")
	assert.Contains(t, err.Error(), "connection reset")

	assert.Empty(t, filesIn(t, tempDir), "a partial download must not leave a file behind")
	assert.True(t, fake.closed, "the object body must be closed even when the read fails")
}

// TestDownload_StreamsIntoTempDir pins the streaming contract: the body is written
// to a file under the configured directory rather than buffered in memory.
func TestDownload_StreamsIntoTempDir(t *testing.T) {
	tempDir := t.TempDir()
	content := []byte("streamed to disk")

	res, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: content}), WithTempDir(tempDir))
	require.NoError(t, err)

	names := filesIn(t, tempDir)
	require.Len(t, names, 1, "the object must be streamed into exactly one file")
	assert.True(t, strings.HasPrefix(names[0], "ocm-s3-download-"), "unexpected file name %q", names[0])

	// The file is the blob's backing store, not a discarded copy.
	onDisk, err := os.ReadFile(filepath.Join(tempDir, names[0]))
	require.NoError(t, err)
	assert.Equal(t, content, onDisk)
	assert.Equal(t, content, readBlob(t, res.Blob))
}

// TestDownload_TempFileOutlivesCall documents the ownership rule: the file backing
// the blob is deliberately not removed when Download returns, so the caller owns it.
func TestDownload_TempFileOutlivesCall(t *testing.T) {
	tempDir := t.TempDir()

	res, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: []byte("still here")}), WithTempDir(tempDir))
	require.NoError(t, err)

	require.Len(t, filesIn(t, tempDir), 1)
	assert.Equal(t, []byte("still here"), readBlob(t, res.Blob))
}

func TestDownload_MaxDownloadSize(t *testing.T) {
	content := []byte("0123456789") // 10 bytes

	tests := []struct {
		name string
		// maxSize is applied via WithMaxDownloadSize unless unset is true.
		maxSize int64
		unset   bool
		// contentLength overrides the reported size, decoupling it from the body
		// so the up-front check and the streaming check can be exercised apart.
		contentLength *int64
		wantErr       bool
	}{
		{name: "body below the limit", maxSize: 100},
		{name: "body exactly at the limit", maxSize: 10},
		{name: "body one byte over the limit", maxSize: 9, wantErr: true},
		{name: "zero disables the limit", maxSize: 0},
		{name: "negative disables the limit", maxSize: -1},
		{name: "unset uses the default", unset: true},
		{
			name:          "oversized object is rejected from its reported length",
			maxSize:       5,
			contentLength: aws.Int64(1 << 30),
			wantErr:       true,
		},
		{
			name:          "understated length is still caught while streaming",
			maxSize:       5,
			contentLength: aws.Int64(1),
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			opts := []Option{
				WithClient(&fakeGetter{body: content, contentLength: tt.contentLength}),
				WithTempDir(tempDir),
			}
			if !tt.unset {
				opts = append(opts, WithMaxDownloadSize(tt.maxSize))
			}

			res, err := Download(t.Context(), Request{BucketName: "my-bucket", ObjectKey: "my-key"}, opts...)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "exceeds maximum allowed size")
				assert.Empty(t, filesIn(t, tempDir), "a rejected download must not leave a file behind")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, content, readBlob(t, res.Blob))
		})
	}
}

// TestDownload_OversizedObjectIsRejectedBeforeTransfer verifies the up-front
// ContentLength check short-circuits before the body is read.
func TestDownload_OversizedObjectIsRejectedBeforeTransfer(t *testing.T) {
	body := &errReader{err: fmt.Errorf("body must not be read")}
	fake := &fakeGetter{
		body:          []byte("0123456789"),
		bodyReader:    body,
		contentLength: aws.Int64(1 << 30),
	}

	_, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(fake), WithMaxDownloadSize(10), WithTempDir(t.TempDir()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum allowed size")
	assert.Zero(t, body.off, "the body must not be read when the reported length already exceeds the limit")
}

func TestDownload_TempDirUnwritable(t *testing.T) {
	_, err := Download(t.Context(), Request{BucketName: "my-bucket", ObjectKey: "my-key"},
		WithClient(&fakeGetter{body: []byte("content")}),
		WithTempDir(filepath.Join(t.TempDir(), "does-not-exist")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error creating temporary file for s3 object my-bucket/my-key")
}

func TestDownload_EmptyObject(t *testing.T) {
	res, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: []byte{}}), WithTempDir(t.TempDir()))
	require.NoError(t, err)
	assert.Empty(t, readBlob(t, res.Blob))
}

func TestDownload_CredentialConversionErrorIsWrapped(t *testing.T) {
	// No client is injected, so the download builds one and must convert the
	// credentials first. An unregistered type cannot convert.
	unknown := &runtime.Raw{Type: runtime.NewVersionedType("Unknown", "v1"), Data: []byte("{}")}

	_, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithCredentials(unknown), WithTempDir(t.TempDir()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error converting s3 credentials")
}

func TestHTTPConfig(t *testing.T) {
	t.Run("nil config yields an empty config", func(t *testing.T) {
		got := httpConfig(nil, false)
		require.NotNil(t, got)
		assert.Nil(t, got.Retry, "nothing configured leaves the transport on its own default")
		assert.Nil(t, got.InsecureSkipVerify)
	})

	t.Run("the caller's retry config reaches the transport", func(t *testing.T) {
		callerRetries := 7
		got := httpConfig(&httpv1alpha1.Config{
			Retry: &httpv1alpha1.RetryConfig{MaxRetries: &callerRetries},
		}, false)
		require.NotNil(t, got.Retry)
		require.NotNil(t, got.Retry.MaxRetries)
		assert.Equal(t, 7, *got.Retry.MaxRetries)
	})

	t.Run("per-host retry reaches the transport too", func(t *testing.T) {
		hostRetries := 3
		got := httpConfig(&httpv1alpha1.Config{
			Hosts: map[string]*httpv1alpha1.HostConfig{
				"s3.amazonaws.com": {Retry: &httpv1alpha1.RetryConfig{MaxRetries: &hostRetries}},
			},
		}, false)

		hc := got.Hosts["s3.amazonaws.com"]
		require.NotNil(t, hc)
		require.NotNil(t, hc.Retry)
		require.NotNil(t, hc.Retry.MaxRetries)
		assert.Equal(t, 3, *hc.Retry.MaxRetries)
	})

	t.Run("a nil host entry is left alone", func(t *testing.T) {
		got := httpConfig(&httpv1alpha1.Config{
			Hosts: map[string]*httpv1alpha1.HostConfig{"s3.amazonaws.com": nil},
		}, false)
		assert.Nil(t, got.Hosts["s3.amazonaws.com"])
	})

	t.Run("insecureSkipTLSVerify is folded into the TLS config", func(t *testing.T) {
		got := httpConfig(nil, true)
		require.NotNil(t, got.InsecureSkipVerify)
		assert.True(t, *got.InsecureSkipVerify)
	})

	t.Run("verification stays enabled when not skipped", func(t *testing.T) {
		got := httpConfig(&httpv1alpha1.Config{}, false)
		assert.Nil(t, got.InsecureSkipVerify, "TLS verification must not be touched unless skipping is requested")
	})

	t.Run("caller timeouts are preserved", func(t *testing.T) {
		timeout := httpv1alpha1.NewTimeout(42)
		got := httpConfig(&httpv1alpha1.Config{
			TimeoutConfig: httpv1alpha1.TimeoutConfig{Timeout: timeout},
		}, false)
		require.NotNil(t, got.Timeout)
		assert.Equal(t, *timeout, *got.Timeout)
	})

	t.Run("the caller's config is not mutated", func(t *testing.T) {
		callerRetries := 7
		cfg := &httpv1alpha1.Config{
			Retry: &httpv1alpha1.RetryConfig{MaxRetries: &callerRetries},
		}
		got := httpConfig(cfg, true)

		assert.Nil(t, cfg.InsecureSkipVerify, "the caller's TLS config must be left alone")
		assert.NotSame(t, cfg, got)
		assert.NotSame(t, cfg.Retry, got.Retry, "the retry config must be copied, not shared")
	})
}

func TestNewClient(t *testing.T) {
	ctx := t.Context()

	t.Run("region defaults when unset", func(t *testing.T) {
		client, err := newClient(ctx, Request{BucketName: "b", ObjectKey: "k"}, &option{})
		require.NoError(t, err)
		assert.Equal(t, defaultRegion, client.Options().Region)
	})

	t.Run("explicit region is used", func(t *testing.T) {
		client, err := newClient(ctx, Request{Region: "eu-central-1"}, &option{})
		require.NoError(t, err)
		assert.Equal(t, "eu-central-1", client.Options().Region)
	})

	t.Run("custom endpoint and path style are applied", func(t *testing.T) {
		client, err := newClient(ctx, Request{
			Endpoint:     "https://minio.internal:9000",
			UsePathStyle: true,
		}, &option{})
		require.NoError(t, err)
		assert.Equal(t, "https://minio.internal:9000", aws.ToString(client.Options().BaseEndpoint))
		assert.True(t, client.Options().UsePathStyle)
	})

	t.Run("no endpoint leaves the base endpoint unset", func(t *testing.T) {
		client, err := newClient(ctx, Request{Region: "us-west-2"}, &option{})
		require.NoError(t, err)
		assert.Nil(t, client.Options().BaseEndpoint, "AWS is targeted when no endpoint is given")
		assert.False(t, client.Options().UsePathStyle)
	})

	t.Run("static credentials are applied", func(t *testing.T) {
		client, err := newClient(ctx, Request{Region: "us-east-1"}, &option{Credentials: &credv1.S3Credentials{
			Type:            credv1.S3CredentialsVersionedType,
			AccessKeyID:     "AKIA",
			SecretAccessKey: "secret",
			SessionToken:    "session",
		}})
		require.NoError(t, err)

		creds, err := client.Options().Credentials.Retrieve(ctx)
		require.NoError(t, err)
		assert.Equal(t, "AKIA", creds.AccessKeyID)
		assert.Equal(t, "secret", creds.SecretAccessKey)
		assert.Equal(t, "session", creds.SessionToken)
	})

	t.Run("credentials without an access key fall through to the default chain", func(t *testing.T) {
		// An empty access key ID means "not statically configured", so the AWS
		// default credential chain must stay in place rather than be replaced
		// with an empty static provider.
		client, err := newClient(ctx, Request{Region: "us-east-1"}, &option{Credentials: &credv1.S3Credentials{
			Type: credv1.S3CredentialsVersionedType,
		}})
		require.NoError(t, err)
		require.NotNil(t, client.Options().Credentials)
	})

	t.Run("unconvertible credentials error", func(t *testing.T) {
		unknown := &runtime.Raw{Type: runtime.NewVersionedType("Unknown", "v1"), Data: []byte("{}")}
		_, err := newClient(ctx, Request{}, &option{Credentials: unknown})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error converting s3 credentials")
	})

	t.Run("an HTTP client is always installed", func(t *testing.T) {
		client, err := newClient(ctx, Request{InsecureSkipTLSVerify: true}, &option{})
		require.NoError(t, err)
		assert.NotNil(t, client.Options().HTTPClient, "the shared ocm HTTP client must back the s3 client")
	})

	t.Run("a supplied HTTP client is used as-is", func(t *testing.T) {
		// An HTTP client carries no bucket, region or endpoint, so injecting one is
		// safe across resources — unlike injecting a pre-built S3 client.
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

	t.Run("the configured retry is left to the transport", func(t *testing.T) {
		// Retry from the OCM config drives the transport. The SDK keeps its own
		// attempt count, because the two layers retry different failures.
		maxRetries := 3
		client, err := newClient(ctx, Request{}, &option{
			HTTPConfig: &httpv1alpha1.Config{Retry: &httpv1alpha1.RetryConfig{MaxRetries: &maxRetries}},
		})
		require.NoError(t, err)
		assert.Zero(t, client.Options().RetryMaxAttempts, "the SDK must stay on its own default")
	})

	t.Run("the SDK retryer is left at its default", func(t *testing.T) {
		client, err := newClient(ctx, Request{}, &option{})
		require.NoError(t, err)
		assert.Zero(t, client.Options().RetryMaxAttempts, "an unset value lets the SDK apply its default")
		assert.Equal(t, retry.DefaultMaxAttempts, client.Options().Retryer.MaxAttempts())
	})
}
