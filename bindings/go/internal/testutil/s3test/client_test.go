package s3test_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/s3test"
)

type recordedRequest struct {
	method, path, query, authorization string
	body                               string
}

type recorder struct {
	mu       sync.Mutex
	requests []recordedRequest
}

func (rec *recorder) all() []recordedRequest {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.requests
}

// recordingServer answers every request with 200, sets x-amz-version-id when versionID
// is not empty, and records what it received.
func recordingServer(t *testing.T, versionID string) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		rec.mu.Lock()
		rec.requests = append(rec.requests, recordedRequest{
			method:        req.Method,
			path:          req.URL.Path,
			query:         req.URL.RawQuery,
			authorization: req.Header.Get("Authorization"),
			body:          string(body),
		})
		rec.mu.Unlock()
		if versionID != "" {
			w.Header().Set("X-Amz-Version-Id", versionID)
		}
	}))
	t.Cleanup(server.Close)
	return server, rec
}

func TestClient(t *testing.T) {
	r := require.New(t)
	server, rec := recordingServer(t, "")

	s3test.CreateBucket(t, t.Context(), s3test.Client(t, server.URL), "bucket")

	requests := rec.all()
	r.Len(requests, 1)
	r.Equal(http.MethodPut, requests[0].method)
	r.Equal("/bucket/", requests[0].path, "path-style puts the bucket in the path, not the host")
	r.Contains(requests[0].authorization, "Credential="+s3test.AccessKey+"/")
	r.Contains(requests[0].authorization, "/"+s3test.Region+"/s3/aws4_request")
}

func TestEnableVersioning(t *testing.T) {
	r := require.New(t)
	server, rec := recordingServer(t, "")

	s3test.EnableVersioning(t, t.Context(), s3test.Client(t, server.URL), "bucket")

	requests := rec.all()
	r.Len(requests, 1)
	r.Equal(http.MethodPut, requests[0].method)
	r.Equal("/bucket/", requests[0].path)
	r.Equal("versioning=", requests[0].query)
	r.Contains(requests[0].body, "<Status>Enabled</Status>")
}

func TestPutObject(t *testing.T) {
	tests := []struct {
		name      string
		versionID string
	}{
		{name: "versioned bucket", versionID: "v1"},
		{name: "unversioned bucket"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			server, rec := recordingServer(t, tt.versionID)

			got := s3test.PutObject(t, t.Context(), s3test.Client(t, server.URL), "bucket", "path/to/key", []byte("content"))

			r.Equal(tt.versionID, got)
			requests := rec.all()
			r.Len(requests, 1)
			r.Equal(http.MethodPut, requests[0].method)
			r.Equal("/bucket/path/to/key", requests[0].path)
			r.Contains(requests[0].body, "content")
		})
	}
}
