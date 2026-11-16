package pack_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	. "ocm.software/open-component-model/bindings/go/oci/internal/pack"

	"github.com/opencontainers/go-digest"
	"github.com/oras-project/oras-go/v3/registry/remote"
	"github.com/stretchr/testify/require"

	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ocmhttp "ocm.software/open-component-model/bindings/go/http"
	resourceblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/internal/remotestore"
	oci "ocm.software/open-component-model/bindings/go/oci/spec/access"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// readCountingBlob counts how often its content is read, like a download that is requested
// when its body is first read.
type readCountingBlob struct {
	content []byte
	reads   atomic.Int32
}

func (b *readCountingBlob) ReadCloser() (io.ReadCloser, error) {
	return &readCountingReader{Reader: bytes.NewReader(b.content), blob: b}, nil
}

type readCountingReader struct {
	io.Reader
	blob    *readCountingBlob
	started bool
}

func (r *readCountingReader) Read(p []byte) (int, error) {
	if !r.started {
		r.started = true
		r.blob.reads.Add(1)
	}
	return r.Reader.Read(p)
}

func (*readCountingReader) Close() error { return nil }

// A registry that fails an upload with a retryable status must not fail the push of a
// spooled blob: the HTTP client sends the upload again, which it only does for a body it can
// rewind. The blob is then read from the spool file, not from its source a second time.
func TestResourceLocalBlobOCILayer_ResendsSpooledBlobWhenTheUploadIsRetried(t *testing.T) {
	r := require.New(t)
	data := bytes.Repeat([]byte("spooled blob content "), 50_000)

	var (
		uploads  atomic.Int32
		mu       sync.Mutex
		received [][]byte
	)
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.Method {
		case http.MethodPost:
			w.Header().Set("Location", req.URL.Path+"session")
			w.WriteHeader(http.StatusAccepted)
		case http.MethodPut:
			body, _ := io.ReadAll(req.Body)
			mu.Lock()
			received = append(received, body)
			mu.Unlock()
			if uploads.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(registry.Close)

	repo, err := remote.NewRepository(registry.Listener.Addr().String() + "/test-repo")
	r.NoError(err)
	repo.Registry.PlainHTTP = true
	repo.Registry.Client = ocmhttp.New()
	store := &remotestore.RemoteStore{Repository: repo}

	opts := Options{AccessScheme: runtime.NewScheme(), BaseReference: "test-ref", TempDir: t.TempDir()}
	v2.MustAddToScheme(opts.AccessScheme)
	oci.MustAddToScheme(opts.AccessScheme)
	source := &readCountingBlob{content: data}
	resourceBlob, err := resourceblob.NewArtifactBlob(&descriptor.Resource{}, source)
	r.NoError(err)

	desc, err := ResourceLocalBlobOCILayer(t.Context(), store, resourceBlob, &v2.LocalBlob{MediaType: "application/octet-stream"}, opts)
	r.NoError(err)
	r.Equal(digest.FromBytes(data), desc.Digest)
	mu.Lock()
	defer mu.Unlock()
	r.Len(received, 2, "the upload is sent a second time")
	for _, body := range received {
		r.True(bytes.Equal(data, body), "every upload carries the complete blob")
	}
	r.EqualValues(1, source.reads.Load(), "the source is read once")
}
