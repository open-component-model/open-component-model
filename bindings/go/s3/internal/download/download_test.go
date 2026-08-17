package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	httpv1alpha1 "ocm.software/open-component-model/bindings/go/http/spec/config/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
	credv1 "ocm.software/open-component-model/bindings/go/s3/spec/credentials/v1"
)

// fakeGetter is a stand-in S3 client returning canned object content and recording
// the input it was called with.
type fakeGetter struct {
	body          []byte
	contentType   string
	contentLength *int64
	bodyReader    io.ReadCloser
	versionID     string
	err           error

	gotInput *s3.GetObjectInput
	closed   bool
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
		ContentLength: new(length),
	}
	if f.contentType != "" {
		out.ContentType = new(f.contentType)
	}
	if f.versionID != "" {
		out.VersionId = new(f.versionID)
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

	assert.Equal(t, int64(len(content)), res.Blob.Size())
}

func TestDownload_PinnedVersionIsForwarded(t *testing.T) {
	fake := &fakeGetter{body: []byte("versioned")}

	_, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k", Version: "v-1"},
		WithClient(fake), WithTempDir(t.TempDir()))
	require.NoError(t, err)

	assert.Equal(t, "v-1", aws.ToString(fake.gotInput.VersionId))
}

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

			got, known := res.Blob.MediaType()
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

func TestDownload_StreamsIntoTempDir(t *testing.T) {
	tempDir := t.TempDir()
	content := []byte("streamed to disk")

	res, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: content}), WithTempDir(tempDir))
	require.NoError(t, err)

	names := filesIn(t, tempDir)
	require.Len(t, names, 1, "the object must be streamed into exactly one file")
	assert.True(t, strings.HasPrefix(names[0], "ocm-s3-download-"), "unexpected file name %q", names[0])

	onDisk, err := os.ReadFile(filepath.Join(tempDir, names[0]))
	require.NoError(t, err)
	assert.Equal(t, content, onDisk)
	assert.Equal(t, content, readBlob(t, res.Blob))
}

func TestDownload_TempFileOutlivesCall(t *testing.T) {
	tempDir := t.TempDir()

	res, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: []byte("still here")}), WithTempDir(tempDir))
	require.NoError(t, err)

	require.Len(t, filesIn(t, tempDir), 1)
	assert.Equal(t, []byte("still here"), readBlob(t, res.Blob))
}

func TestDownload_CloseRemovesTempFile(t *testing.T) {
	tempDir := t.TempDir()

	res, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithClient(&fakeGetter{body: []byte("payload")}), WithTempDir(tempDir))
	require.NoError(t, err)
	require.Len(t, filesIn(t, tempDir), 1)

	require.NoError(t, res.Blob.Close())
	assert.Empty(t, filesIn(t, tempDir), "closing the blob must remove the temporary file")

	assert.NoError(t, res.Blob.Close(), "closing twice must not fail")
}

// A blob dropped without being closed must not keep its file for the lifetime of the
// process.
func TestDownload_AbandonedBlobIsReclaimed(t *testing.T) {
	tempDir := t.TempDir()

	func() {
		_, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
			WithClient(&fakeGetter{body: []byte("payload")}), WithTempDir(tempDir))
		require.NoError(t, err)
		require.Len(t, filesIn(t, tempDir), 1)
	}()

	// The cleanup runs asynchronously once the blob is unreachable, so collect and poll.
	require.Eventually(t, func() bool {
		goruntime.GC()
		entries, err := os.ReadDir(tempDir)
		return err == nil && len(entries) == 0
	}, 10*time.Second, 20*time.Millisecond, "an abandoned download must not leave its temporary file behind")
}

// The cleanup must not pull the file out from under a read still in progress.
func TestDownload_OpenReaderSurvivesCollection(t *testing.T) {
	content := []byte("payload that is read after the blob goes out of scope")

	rc := func() io.ReadCloser {
		res, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
			WithClient(&fakeGetter{body: content}), WithTempDir(t.TempDir()))
		require.NoError(t, err)

		rc, err := res.Blob.ReadCloser()
		require.NoError(t, err)
		return rc
	}()
	t.Cleanup(func() { _ = rc.Close() })

	goruntime.GC()

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

func TestDownload_MaxDownloadSize(t *testing.T) {
	content := []byte("0123456789")

	tests := []struct {
		name          string
		maxSize       int64
		unset         bool
		contentLength *int64
		wantErr       bool
	}{
		{name: "body below the limit", maxSize: 100},
		{name: "body exactly at the limit", maxSize: 10},
		{name: "body one byte over the limit", maxSize: 9, wantErr: true},
		{name: "zero disables the limit", maxSize: 0},
		{name: "negative disables the limit", maxSize: -1},
		{name: "unset uses the default", unset: true},
		{name: "the largest limit does not overflow into a truncated read", maxSize: math.MaxInt64},
		{
			name:          "oversized object is rejected from its reported length",
			maxSize:       5,
			contentLength: new(int64(1 << 30)),
			wantErr:       true,
		},
		{
			name:          "understated length is still caught while streaming",
			maxSize:       5,
			contentLength: new(int64(1)),
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

func TestDownload_OversizedObjectIsRejectedBeforeTransfer(t *testing.T) {
	body := &errReader{err: fmt.Errorf("body must not be read")}
	fake := &fakeGetter{
		body:          []byte("0123456789"),
		bodyReader:    body,
		contentLength: new(int64(1 << 30)),
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
	unknown := &runtime.Raw{Type: runtime.NewVersionedType("Unknown", "v1"), Data: []byte("{}")}

	_, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k"},
		WithCredentials(unknown), WithTempDir(t.TempDir()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "error converting s3 credentials")
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
		assert.Equal(t, 7, *cfg.Retry.MaxRetries, "the caller keeps the retry count it configured")
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

	t.Run("empty credentials fall through to the default chain", func(t *testing.T) {
		client, err := newClient(ctx, Request{Region: "us-east-1"}, &option{Credentials: &credv1.S3Credentials{
			Type: credv1.S3CredentialsVersionedType,
		}})
		require.NoError(t, err)
		require.NotNil(t, client.Options().Credentials)
	})

	// A half-filled credential cannot sign a request, and the SDK says so. What matters
	// here is that it reaches the SDK at all: dropping it would send the request under
	// whatever identity the default credential chain happens to resolve.
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

	t.Run("static credentials without a session token are applied", func(t *testing.T) {
		client, err := newClient(ctx, Request{Region: "us-east-1"}, &option{Credentials: &credv1.S3Credentials{
			Type:            credv1.S3CredentialsVersionedType,
			AccessKeyID:     "AKIA",
			SecretAccessKey: "secret",
		}})
		require.NoError(t, err)

		creds, err := client.Options().Credentials.Retrieve(ctx)
		require.NoError(t, err)
		assert.Equal(t, "AKIA", creds.AccessKeyID)
		assert.Empty(t, creds.SessionToken)
	})

	t.Run("unconvertible credentials error", func(t *testing.T) {
		unknown := &runtime.Raw{Type: runtime.NewVersionedType("Unknown", "v1"), Data: []byte("{}")}
		_, err := newClient(ctx, Request{}, &option{Credentials: unknown})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error converting s3 credentials")
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
