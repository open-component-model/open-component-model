package transformation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
	wgetaccess "ocm.software/open-component-model/bindings/go/wget/spec/access"
	wgetaccessv1 "ocm.software/open-component-model/bindings/go/wget/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/wget/transformation/spec/v1alpha1"
)

// stubResourceRepository serves a fixed in-memory payload for DownloadResource and
// records whether a temp file was ever requested (it never is for an in-memory blob).
type stubResourceRepository struct {
	payload   []byte
	mediaType string
}

func (s *stubResourceRepository) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (s *stubResourceRepository) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	return inmemory.New(strings.NewReader(string(s.payload)), inmemory.WithMediaType(s.mediaType), inmemory.WithSize(int64(len(s.payload)))), nil
}

func (s *stubResourceRepository) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return nil, nil
}

func newTransformerScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(v1alpha1.Scheme)
	scheme.MustRegisterScheme(wgetaccess.Scheme)
	scheme.MustRegisterScheme(v2.Scheme)
	return scheme
}

func wgetResourceV2(name, url, verb, mediaType string, header map[string][]string, digest *v2.Digest) *v2.Resource {
	access := &wgetaccessv1.Wget{
		Type:      wgetaccess.V1VersionedType,
		URL:       url,
		Verb:      verb,
		Header:    header,
		MediaType: mediaType,
	}
	raw := &runtime.Raw{}
	if err := wgetaccess.Scheme.Convert(access, raw); err != nil {
		panic(err)
	}
	return &v2.Resource{
		ElementMeta: v2.ElementMeta{ObjectMeta: v2.ObjectMeta{Name: name, Version: "1.0.0"}},
		Type:        "blob",
		Relation:    v2.ExternalRelation,
		Access:      raw,
		Digest:      digest,
	}
}

func runStreaming(t *testing.T, payload []byte, srcDigest *v2.Digest, recordedBody *[]byte, recordedMethod, recordedHeader, recordedContentType *string) (runtime.Typed, error) {
	t.Helper()
	scheme := newTransformerScheme()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*recordedMethod = r.Method
		*recordedHeader = r.Header.Get("X-Custom")
		*recordedContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		*recordedBody = body
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	source := wgetResourceV2("blob", "https://source.example/blob.tar", "", "", nil, srcDigest)
	target := wgetResourceV2("blob", srv.URL+"/target/blob.tar", http.MethodPut, "application/x-tar", map[string][]string{"X-Custom": {"hello"}}, nil)

	step := &v1alpha1.HTTPStreaming{
		Type: v1alpha1.HTTPStreamingV1alpha1,
		ID:   "upload",
		Spec: &v1alpha1.HTTPStreamingSpec{Resource: source, TargetResource: target},
	}

	tr := &HTTPStreamingTransformer{
		Scheme:             scheme,
		ResourceRepository: &stubResourceRepository{payload: payload, mediaType: "application/x-tar"},
	}
	return tr.Transform(context.Background(), step)
}

func TestHTTPStreamingTransformer_ComputesDigestDuringStream(t *testing.T) {
	r := require.New(t)
	payload := []byte("hello uploader world")
	var body []byte
	var method, header, contentType string

	out, err := runStreaming(t, payload, nil, &body, &method, &header, &contentType)
	r.NoError(err)

	assert.Equal(t, http.MethodPut, method)
	assert.Equal(t, "hello", header, "custom header from the target wget access must be sent")
	assert.Equal(t, "application/x-tar", contentType)
	assert.Equal(t, payload, body, "the exact payload must be streamed to the PUT target")

	result := out.(*v1alpha1.HTTPStreaming)
	r.NotNil(result.Output)
	r.NotNil(result.Output.Resource)
	r.NotNil(result.Output.Resource.Digest)

	sum := sha256.Sum256(payload)
	assert.Equal(t, hex.EncodeToString(sum[:]), result.Output.Resource.Digest.Value)

	tw := wgetaccessv1.Wget{}
	r.NoError(wgetaccess.Scheme.Convert(result.Output.Resource.Access, &tw))
	assert.Equal(t, "Wget", tw.GetType().Name)
	assert.Contains(t, tw.URL, "/target/blob.tar")
}

func TestHTTPStreamingTransformer_VerifiesMatchingDigest(t *testing.T) {
	r := require.New(t)
	payload := []byte("verify me")
	dig := godigest.FromBytes(payload)
	srcDigest := &v2.Digest{
		HashAlgorithm:          hashAlgorithmSHA256,
		NormalisationAlgorithm: genericBlobDigestV1,
		Value:                  dig.Encoded(),
	}
	var body []byte
	var method, header, contentType string

	out, err := runStreaming(t, payload, srcDigest, &body, &method, &header, &contentType)
	r.NoError(err)
	result := out.(*v1alpha1.HTTPStreaming)
	assert.Equal(t, dig.Encoded(), result.Output.Resource.Digest.Value)
}

func TestHTTPStreamingTransformer_RejectsMismatchedDigest_AfterStreaming(t *testing.T) {
	r := require.New(t)
	payload := []byte("streamed content")
	srcDigest := &v2.Digest{
		HashAlgorithm:          hashAlgorithmSHA256,
		NormalisationAlgorithm: genericBlobDigestV1,
		Value:                  "0000000000000000000000000000000000000000000000000000000000000000",
	}
	var body []byte
	var method, header, contentType string

	_, err := runStreaming(t, payload, srcDigest, &body, &method, &header, &contentType)
	r.Error(err)
	assert.Contains(t, err.Error(), "digest mismatch")
	// The verification happens after streaming: the server still received the full payload,
	// proving the content was piped through rather than buffered and checked up front.
	assert.Equal(t, payload, body)
}
