package download

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// streamFrom builds a lazy S3 stream against srv with [fakeCredentials], addressed
// path-style like [downloadFrom].
func streamFrom(t *testing.T, srv *fakeS3, req Request, opts ...Option) *Stream {
	t.Helper()
	req.Endpoint = srv.URL
	req.UsePathStyle = true
	return NewStream(t.Context(), req, append([]Option{WithCredentials(fakeCredentials())}, opts...)...)
}

func TestStream_ReturnsObjectBodyAndIsReplayable(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	content := []byte("hello from s3 stream")
	srv := newFakeS3(t, s3Object{body: content, contentType: "text/plain"})

	s := streamFrom(t, srv, Request{BucketName: "my-bucket", ObjectKey: "path/blob.txt"})

	r.Equal(content, readBlob(t, s))
	// A second ReadCloser re-issues GetObject and starts from the beginning.
	r.Equal(content, readBlob(t, s))

	requests := srv.recorded()
	r.Len(requests, 2, "each read must issue its own GetObject")
	for _, req := range requests {
		assert.Equal(t, http.MethodGet, req.method)
		assert.Equal(t, "/my-bucket/path/blob.txt", req.path)
	}

	mt, known := s.MediaType()
	r.True(known)
	r.Equal("text/plain", mt)
}

func TestStream_RequiredFields(t *testing.T) {
	t.Parallel()
	srv := newFakeS3(t, s3Object{body: []byte("unused")})

	t.Run("missing bucket", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		s := streamFrom(t, srv, Request{ObjectKey: "k"})
		_, err := s.ReadCloser()
		r.ErrorContains(err, "bucketName is required")
	})
	t.Run("missing key", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		s := streamFrom(t, srv, Request{BucketName: "b"})
		_, err := s.ReadCloser()
		r.ErrorContains(err, "objectKey is required")
	})
}

func TestStream_PinnedVersionIsForwarded(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	srv := newFakeS3(t, s3Object{body: []byte("versioned")})

	s := streamFrom(t, srv, Request{BucketName: "b", ObjectKey: "k", Version: "v-1"})
	_ = readBlob(t, s)

	requests := srv.recorded()
	r.Len(requests, 1)
	r.Equal("v-1", requests[0].versionID)
}

func TestStream_MaxDownloadSize(t *testing.T) {
	t.Parallel()
	content := []byte("0123456789") // 10 bytes

	tests := []struct {
		name    string
		maxSize int64
		object  s3Object
		wantErr bool
	}{
		{name: "body below the limit", maxSize: 100},
		{name: "body exactly at the limit", maxSize: 10},
		{
			name:    "oversized object rejected from its reported length",
			maxSize: 5,
			object:  s3Object{declaredLength: new(int64(1 << 30)), suppressBody: true},
			wantErr: true,
		},
		{
			name:    "a store reporting no length is caught while streaming",
			maxSize: 5,
			object:  s3Object{chunked: true},
			wantErr: true,
		},
		{name: "zero disables the limit", maxSize: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)
			object := tt.object
			if object.body == nil {
				object.body = content
			}
			srv := newFakeS3(t, object)

			s := streamFrom(t, srv, Request{BucketName: "my-bucket", ObjectKey: "my-key"},
				WithMaxDownloadSize(tt.maxSize))

			rc, err := s.ReadCloser()
			if tt.wantErr && err != nil {
				assert.Contains(t, err.Error(), "exceeds maximum allowed size")
				return
			}
			r.NoError(err)
			_, readErr := io.ReadAll(rc)
			closeErr := rc.Close()
			if tt.wantErr {
				if readErr == nil {
					readErr = closeErr
				}
				r.Error(readErr)
				assert.Contains(t, readErr.Error(), "exceeds maximum allowed size")
				return
			}
			r.NoError(readErr)
		})
	}
}

func TestStream_GetObjectErrorIsWrapped(t *testing.T) {
	t.Parallel()
	r := require.New(t)
	srv := newFakeS3(t, s3Object{errStatus: http.StatusForbidden, errCode: "AccessDenied"})

	s := streamFrom(t, srv, Request{BucketName: "my-bucket", ObjectKey: "my-key"})
	_, err := s.ReadCloser()
	r.Error(err)
	r.ErrorContains(err, "error getting s3 object my-bucket/my-key")
	r.ErrorContains(err, "AccessDenied")
}
