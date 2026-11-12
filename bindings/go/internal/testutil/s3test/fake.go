package s3test

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
)

// Object is the canned response a [Fake] serves for a GetObject or HeadObject request.
type Object struct {
	// Body is the object content.
	Body []byte
	// ContentType is served as the Content-Type header. Empty sends none, which is
	// what an object stored without one looks like.
	ContentType string
	// VersionID is served as the x-amz-version-id header. Empty sends none, as a
	// store that does not report versions does.
	VersionID string
	// ErrStatus and ErrCode serve an S3 error in place of the object.
	ErrStatus int
	ErrCode   string
	// Chunked serves the body without a Content-Length, which is how a store that
	// does not know the object size up front answers.
	Chunked bool
	// AbortAfter drops the connection after that many bytes of body, simulating a
	// store failing mid-object. It implies Chunked, since a length cannot be declared
	// for a body that is never finished.
	AbortAfter *int
	// SuppressBody serves the headers of an object but none of it, telling a caller that
	// decides from the reported length apart from one that reads first.
	SuppressBody bool
	// DeclaredLength overrides the Content-Length header. Nil reports the true length
	// of Body. It cannot understate the real length: the transport enforces the declared
	// count, so a store cannot hand out more than it announced.
	DeclaredLength *int64
}

// Request is one request a [Fake] answered.
type Request struct {
	Method string
	// Path is the request path, which is /bucket/key for path-style addressing.
	Path string
	// VersionID is the versionId query parameter, empty when none was sent.
	VersionID string
	Header    http.Header
}

// Fake is an httptest server answering S3 object requests with a canned [Object], so
// that the code under test goes through the real AWS SDK: the SDK signs, addresses and
// parses the request, and only the store is fake. It serves no bucket subdomains, so
// clients must use path-style addressing. It verifies no signature, but clients should
// still pass static credentials, which keeps the AWS default credential chain and its
// instance-metadata lookups out of the tests:
//
//	srv := s3test.StartFake(t, s3test.Object{Body: []byte("content"), VersionID: "v-1"})
//	// point the S3 client at srv.URL with path-style addressing, then
//	requests := srv.Recorded()
type Fake struct {
	*httptest.Server

	mu       sync.Mutex
	object   Object
	requests []Request
}

// StartFake starts a [Fake] serving object and stops it when the test ends.
func StartFake(t *testing.T, object Object) *Fake {
	t.Helper()

	f := &Fake{object: object}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)

	return f
}

// Serve makes the server answer with object from now on.
func (f *Fake) Serve(object Object) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.object = object
}

// Recorded returns the requests the server answered, in the order it answered them.
func (f *Fake) Recorded() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]Request(nil), f.requests...)
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, Request{
		Method:    r.Method,
		Path:      r.URL.Path,
		VersionID: r.URL.Query().Get("versionId"),
		Header:    r.Header.Clone(),
	})
	object := f.object
	f.mu.Unlock()

	if object.ErrStatus != 0 {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(object.ErrStatus)
		fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><Error><Code>%s</Code></Error>`, object.ErrCode)
		return
	}

	if object.ContentType != "" {
		w.Header().Set("Content-Type", object.ContentType)
	} else {
		// A nil entry suppresses the type the server would otherwise sniff from the
		// body, so that an object stored without a content type stays without one.
		w.Header()["Content-Type"] = nil
	}
	if object.VersionID != "" {
		w.Header().Set("X-Amz-Version-Id", object.VersionID)
	}

	// Real S3 reports a checksum, and the SDK warns about every response carrying
	// none. A body that is cut short or never sent cannot be summed.
	if !object.SuppressBody && object.AbortAfter == nil {
		w.Header().Set("X-Amz-Checksum-Crc32", checksumCRC32(object.Body))
	}

	length := int64(len(object.Body))
	if object.DeclaredLength != nil {
		length = *object.DeclaredLength
	}

	// Flushing before the body is written is what makes the response chunked: the headers
	// go out before the length is known. An unflushed one has its Content-Length filled in.
	if object.Chunked || object.AbortAfter != nil {
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
	} else {
		w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
		w.WriteHeader(http.StatusOK)
	}

	if object.SuppressBody {
		return
	}

	body := object.Body
	if n := object.AbortAfter; n != nil {
		if *n < len(body) {
			body = body[:*n]
		}
		_, _ = w.Write(body)
		w.(http.Flusher).Flush()
		// ErrAbortHandler is the panic the server swallows silently rather than logging
		// as a failure.
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
