package s3test_test

import (
	"encoding/base64"
	"encoding/binary"
	"hash/crc32"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/internal/testutil/s3test"
)

func do(t *testing.T, method, url string) (*http.Response, []byte, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "signed")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return resp, body, err
}

func TestFake(t *testing.T) {
	content := []byte("content")
	sum := make([]byte, 4)
	binary.BigEndian.PutUint32(sum, crc32.ChecksumIEEE(content))
	checksum := base64.StdEncoding.EncodeToString(sum)

	t.Run("an object is served with its headers and the request is recorded", func(t *testing.T) {
		r := require.New(t)
		srv := s3test.StartFake(t, s3test.Object{Body: content, ContentType: "text/plain", VersionID: "v-1"})

		resp, body, err := do(t, http.MethodGet, srv.URL+"/bucket/key?versionId=v-1")
		r.NoError(err)
		r.Equal(http.StatusOK, resp.StatusCode)
		r.Equal(content, body)
		r.Equal(int64(len(content)), resp.ContentLength)
		r.Equal("text/plain", resp.Header.Get("Content-Type"))
		r.Equal("v-1", resp.Header.Get("X-Amz-Version-Id"))
		r.Equal(checksum, resp.Header.Get("X-Amz-Checksum-Crc32"))

		requests := srv.Recorded()
		r.Len(requests, 1)
		r.Equal(http.MethodGet, requests[0].Method)
		r.Equal("/bucket/key", requests[0].Path)
		r.Equal("v-1", requests[0].VersionID)
		r.Equal("signed", requests[0].Header.Get("Authorization"))
	})

	t.Run("an object without content type or version sends neither", func(t *testing.T) {
		r := require.New(t)
		srv := s3test.StartFake(t, s3test.Object{Body: content})

		resp, _, err := do(t, http.MethodGet, srv.URL+"/bucket/key")
		r.NoError(err)
		r.NotContains(resp.Header, "Content-Type")
		r.NotContains(resp.Header, "X-Amz-Version-Id")
		r.Empty(srv.Recorded()[0].VersionID)
	})

	t.Run("an error status is served as an S3 error", func(t *testing.T) {
		r := require.New(t)
		srv := s3test.StartFake(t, s3test.Object{ErrStatus: http.StatusForbidden, ErrCode: "AccessDenied"})

		resp, body, err := do(t, http.MethodGet, srv.URL+"/bucket/key")
		r.NoError(err)
		r.Equal(http.StatusForbidden, resp.StatusCode)
		r.Equal("application/xml", resp.Header.Get("Content-Type"))
		r.Contains(string(body), "<Code>AccessDenied</Code>")
	})

	t.Run("a chunked object declares no length", func(t *testing.T) {
		r := require.New(t)
		srv := s3test.StartFake(t, s3test.Object{Body: content, Chunked: true})

		resp, body, err := do(t, http.MethodGet, srv.URL+"/bucket/key")
		r.NoError(err)
		r.Equal(int64(-1), resp.ContentLength)
		r.Equal(content, body)
	})

	t.Run("an aborted object fails the read", func(t *testing.T) {
		r := require.New(t)
		srv := s3test.StartFake(t, s3test.Object{Body: content, AbortAfter: new(3)})

		resp, body, err := do(t, http.MethodGet, srv.URL+"/bucket/key")
		r.ErrorIs(err, io.ErrUnexpectedEOF)
		r.Equal(content[:3], body)
		r.Empty(resp.Header.Get("X-Amz-Checksum-Crc32"))
	})

	t.Run("a declared length replaces the true one", func(t *testing.T) {
		r := require.New(t)
		srv := s3test.StartFake(t, s3test.Object{DeclaredLength: new(int64(1 << 30)), SuppressBody: true})

		resp, body, err := do(t, http.MethodHead, srv.URL+"/bucket/key")
		r.NoError(err)
		r.Equal(int64(1<<30), resp.ContentLength)
		r.Empty(body)
		r.Equal(http.MethodHead, srv.Recorded()[0].Method)
	})

	t.Run("a served object replaces the previous one", func(t *testing.T) {
		r := require.New(t)
		srv := s3test.StartFake(t, s3test.Object{Body: content})
		srv.Serve(s3test.Object{Body: []byte("other"), VersionID: "v-2"})

		resp, body, err := do(t, http.MethodGet, srv.URL+"/bucket/key")
		r.NoError(err)
		r.Equal([]byte("other"), body)
		r.Equal("v-2", resp.Header.Get("X-Amz-Version-Id"))
	})
}
