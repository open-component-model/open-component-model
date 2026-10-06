package download_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/wget/checksum"
	"ocm.software/open-component-model/bindings/go/wget/internal/download"
)

// sha256Hex returns the lowercase hex SHA-256 of content.
func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// requirePolicy builds a Require policy that verifies against advertised headers,
// failing when nothing is advertised (OnMissing: Fail). It is the body-verification
// counterpart of the wget Require mode.
func requirePolicy() checksum.Policy {
	return checksum.Policy{Sources: checksum.BuiltinSources(), OnMissing: checksum.Fail}
}

func TestStream_HappyPathAndReplayable(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	content := []byte("streamed without a temp file")
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	s := download.NewStream(t.Context(), download.Request{URL: srv.URL}, nil, download.WithClient(srv.Client()))

	// First read.
	r.Equal(content, readBlob(t, s))
	// A second ReadCloser re-issues the request and starts from the beginning.
	r.Equal(content, readBlob(t, s))
	r.Equal(int64(2), hits.Load(), "each ReadCloser call must issue its own request")

	mt, ok := s.MediaType()
	r.True(ok)
	r.Equal("text/plain", mt)
}

func TestStream_MediaTypeFromRequestOverridesResponse(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("x"))
	}))
	t.Cleanup(srv.Close)

	s := download.NewStream(t.Context(), download.Request{URL: srv.URL, MediaType: "application/custom"}, nil,
		download.WithClient(srv.Client()))
	mt, ok := s.MediaType()
	r.True(ok)
	r.Equal("application/custom", mt)
}

func TestStream_VerificationSuccess(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	content := []byte("verify me correctly")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-checksum-sha256", sha256Hex(content))
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	policy := requirePolicy()
	verify := func(headers http.Header, digests map[string]string) error {
		_, _, err := checksum.Resolve(t.Context(), policy, checksum.Input{URL: srv.URL, Headers: headers, Computed: digests})
		return err
	}

	s := download.NewStream(t.Context(), download.Request{URL: srv.URL}, verify,
		download.WithClient(srv.Client()),
		download.WithDigestAlgorithms(download.DigestAlgorithm{Name: checksum.SHA256.OCMName, New: checksum.SHA256.New}))

	rc, err := s.ReadCloser()
	r.NoError(err)
	got, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, got)
	r.NoError(rc.Close())
}

// TestStream_VerificationMismatch is the stream-then-verify digest-mismatch path:
// the body streams out in full, but the advertised checksum does not match, so the
// final read and the close both report the error.
func TestStream_VerificationMismatch(t *testing.T) {
	t.Parallel()
	r := require.New(t)

	content := []byte("the bytes actually served")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Advertise a checksum of different content, so verification must fail.
		w.Header().Set("x-checksum-sha256", sha256Hex([]byte("something else entirely")))
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)

	policy := requirePolicy()
	verify := func(headers http.Header, digests map[string]string) error {
		_, _, err := checksum.Resolve(t.Context(), policy, checksum.Input{URL: srv.URL, Headers: headers, Computed: digests})
		return err
	}

	s := download.NewStream(t.Context(), download.Request{URL: srv.URL}, verify,
		download.WithClient(srv.Client()),
		download.WithDigestAlgorithms(download.DigestAlgorithm{Name: checksum.SHA256.OCMName, New: checksum.SHA256.New}))

	rc, err := s.ReadCloser()
	r.NoError(err)
	// io.ReadAll drains to the end; the mismatch surfaces from the final Read.
	_, err = io.ReadAll(rc)
	r.Error(err)
	r.Contains(err.Error(), "checksum mismatch")
	// Close reports the same cached error, so a caller that only checks Close still fails.
	r.Error(rc.Close())
}

func TestStream_MaxDownloadSize(t *testing.T) {
	t.Parallel()

	content := []byte("0123456789") // 10 bytes

	tests := []struct {
		name    string
		maxSize int64
		chunked bool
		wantErr bool
	}{
		{name: "body below the limit", maxSize: 100},
		{name: "body exactly at the limit", maxSize: 10},
		{name: "body one byte over the limit (announced length)", maxSize: 9, wantErr: true},
		{name: "oversized chunked body caught while streaming", maxSize: 9, chunked: true, wantErr: true},
		{name: "zero disables the limit", maxSize: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := require.New(t)

			srv := serveBody(t, content, tt.chunked)
			s := download.NewStream(t.Context(), download.Request{URL: srv.URL}, nil,
				download.WithClient(srv.Client()), download.WithMaxDownloadSize(tt.maxSize))

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
