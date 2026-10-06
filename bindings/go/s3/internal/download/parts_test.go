package download

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// multipartStore is an httptest server holding one multipart object. It answers GETs by
// part number, by open-ended Range, and for the whole object, the way S3 does.
type multipartStore struct {
	*httptest.Server

	parts [][]byte
	// reportPartsCount sends x-amz-mp-parts-count; etagSuffix ends the ETag in
	// "-<parts>". Without either, a client cannot tell the part count.
	reportPartsCount, etagSuffix bool
	versionID                    string
	// partNumberUnsupported answers 501 NotImplemented to any part number.
	partNumberUnsupported bool
	// overwritten makes every request after the first find a different object.
	overwritten bool
	// corruptPart serves that part with a checksum of other bytes.
	corruptPart int
	// misplacedPart claims, for that part, the span of part 1.
	misplacedPart int

	mu       sync.Mutex
	requests []*http.Request
}

func newMultipartStore(t *testing.T, s *multipartStore) *multipartStore {
	t.Helper()
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *multipartStore) content() []byte { return bytes.Join(s.parts, nil) }

func (s *multipartStore) recorded() []*http.Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*http.Request(nil), s.requests...)
}

func (s *multipartStore) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.requests = append(s.requests, r.Clone(r.Context()))
	later := len(s.requests) > 1
	s.mu.Unlock()

	fail := func(status int, code string) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code></Error>`, code)
	}

	etag := `"0123456789abcdef"`
	if s.etagSuffix {
		etag = fmt.Sprintf(`"0123456789abcdef-%d"`, len(s.parts))
	}
	if s.overwritten && later {
		if r.Header.Get("If-Match") != "" {
			fail(http.StatusPreconditionFailed, "PreconditionFailed")
			return
		}
		etag = `"overwritten"`
	}
	w.Header().Set("ETag", etag)
	if s.versionID != "" {
		w.Header().Set("x-amz-version-id", s.versionID)
	}
	w.Header()["Content-Type"] = nil
	content := s.content()
	total := len(content)

	serveSpan := func(start, end int, body []byte) {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, total))
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	}

	if q := r.URL.Query().Get("partNumber"); q != "" {
		if s.partNumberUnsupported {
			fail(http.StatusNotImplemented, "NotImplemented")
			return
		}
		n, _ := strconv.Atoi(q)
		if n < 1 || n > len(s.parts) {
			fail(http.StatusBadRequest, "InvalidPart")
			return
		}
		start := len(bytes.Join(s.parts[:n-1], nil))
		part := s.parts[n-1]
		if s.reportPartsCount {
			w.Header().Set("x-amz-mp-parts-count", strconv.Itoa(len(s.parts)))
		}
		sum := part
		if n == s.corruptPart {
			sum = []byte("other bytes")
		}
		w.Header().Set("x-amz-checksum-crc32", checksumCRC32(sum))
		w.Header().Set("x-amz-checksum-type", "COMPOSITE")
		if n == s.misplacedPart {
			start = 0
		}
		serveSpan(start, start+len(part)-1, part)
		return
	}
	if rng := r.Header.Get("Range"); rng != "" {
		from, _ := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(rng, "bytes="), "-"))
		serveSpan(from, total-1, content[from:])
		return
	}
	w.Header().Set("x-amz-checksum-crc32", checksumCRC32(content))
	w.Header().Set("Content-Length", strconv.Itoa(total))
	_, _ = w.Write(content)
}

func downloadFromStore(t *testing.T, s *multipartStore, opts ...Option) (*Result, string, error) {
	t.Helper()
	withoutAWSEnvironment(t)
	dir := t.TempDir()
	out, err := Download(t.Context(), Request{BucketName: "b", ObjectKey: "k", Endpoint: s.URL, UsePathStyle: true},
		append([]Option{anonymous(), WithTempDir(dir)}, opts...)...)
	return out, dir, err
}

func threeParts() [][]byte {
	return [][]byte{[]byte("first part "), []byte("second part "), []byte("tail")}
}

func TestDownload_Multipart(t *testing.T) {
	tests := []struct {
		name                         string
		reportPartsCount, etagSuffix bool
		wantPartNumbers              []string
		wantRange                    string
	}{
		{name: "part count reported", reportPartsCount: true, wantPartNumbers: []string{"1", "2", "3"}},
		{name: "part count from the ETag suffix", etagSuffix: true, wantPartNumbers: []string{"1", "2", "3"}},
		{name: "unknown part count fetches the rest as one range", wantPartNumbers: []string{"1"}, wantRange: "bytes=11-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			s := newMultipartStore(t, &multipartStore{parts: threeParts(), reportPartsCount: tt.reportPartsCount, etagSuffix: tt.etagSuffix})

			out, _, err := downloadFromStore(t, s)
			r.NoError(err)
			r.Equal(s.content(), readBlob(t, out.Blob))

			var partNumbers []string
			var ranges []string
			for i, req := range s.recorded() {
				if p := req.URL.Query().Get("partNumber"); p != "" {
					partNumbers = append(partNumbers, p)
				}
				if rng := req.Header.Get("Range"); rng != "" {
					ranges = append(ranges, rng)
				}
				r.Equal("ENABLED", req.Header.Get("X-Amz-Checksum-Mode"), "parts are verified against their stored checksums")
				if i > 0 {
					r.NotEmpty(req.Header.Get("If-Match"), "later requests are held to the object part 1 came from")
				}
			}
			r.ElementsMatch(tt.wantPartNumbers, partNumbers)
			if tt.wantRange != "" {
				r.Equal([]string{tt.wantRange}, ranges)
			} else {
				r.Empty(ranges)
			}
		})
	}
}

func TestDownload_MultipartPinsTheVersionPartOneCameFrom(t *testing.T) {
	r := require.New(t)
	s := newMultipartStore(t, &multipartStore{parts: threeParts(), reportPartsCount: true, versionID: "v-1"})

	out, _, err := downloadFromStore(t, s)
	r.NoError(err)
	r.Equal("v-1", out.VersionID)
	requests := s.recorded()
	r.Len(requests, 3)
	r.Empty(requests[0].URL.Query().Get("versionId"), "an unpinned request reads the latest version")
	for _, req := range requests[1:] {
		r.Equal("v-1", req.URL.Query().Get("versionId"))
	}
}

func TestDownload_MultipartFailures(t *testing.T) {
	tests := []struct {
		name    string
		store   *multipartStore
		opts    []Option
		wantErr string
		// wantRequests bounds what was fetched before the failure; zero skips the check.
		wantRequests int
	}{
		{
			name:    "object overwritten mid-download",
			store:   &multipartStore{reportPartsCount: true, overwritten: true},
			wantErr: "PreconditionFailed",
		},
		{
			name:    "part failing its stored checksum",
			store:   &multipartStore{reportPartsCount: true, corruptPart: 2},
			wantErr: "checksum",
		},
		{
			name:    "part claiming another part's span",
			store:   &multipartStore{reportPartsCount: true, misplacedPart: 3},
			wantErr: "do not tile",
		},
		{
			name:         "object larger than the limit is refused before the other parts",
			store:        &multipartStore{reportPartsCount: true},
			opts:         []Option{WithMaxDownloadSize(20)},
			wantErr:      "exceeds maximum allowed size",
			wantRequests: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			store := tt.store
			store.parts = threeParts()
			s := newMultipartStore(t, store)

			out, dir, err := downloadFromStore(t, s, tt.opts...)
			r.Nil(out)
			r.ErrorContains(err, tt.wantErr)
			if tt.wantRequests > 0 {
				r.Len(s.recorded(), tt.wantRequests)
			}
			entries, err := os.ReadDir(dir)
			r.NoError(err)
			r.Empty(entries, "a failed download leaves no file behind")
		})
	}
}

// A store that rejects part numbers is asked for the whole object instead.
func TestDownload_PartNumberUnsupported(t *testing.T) {
	r := require.New(t)
	s := newMultipartStore(t, &multipartStore{parts: threeParts(), partNumberUnsupported: true})

	out, _, err := downloadFromStore(t, s)
	r.NoError(err)
	r.Equal(s.content(), readBlob(t, out.Blob))
	requests := s.recorded()
	r.Len(requests, 2)
	r.Equal("1", requests[0].URL.Query().Get("partNumber"))
	r.Empty(requests[1].URL.Query().Get("partNumber"))
}
