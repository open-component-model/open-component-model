package transformation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
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
	"ocm.software/open-component-model/bindings/go/repository"
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

func wgetRequest(url, verb, mediaType string, header map[string][]string) *wgetaccessv1.Wget {
	return &wgetaccessv1.Wget{
		Type:      wgetaccess.V1VersionedType,
		URL:       url,
		Verb:      verb,
		Header:    header,
		MediaType: mediaType,
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
	// The published (download) access is a plain read access; the upload request carries
	// the write verb and custom header.
	target := wgetResourceV2("blob", srv.URL+"/target/blob.tar", "", "application/x-tar", nil, nil)
	request := wgetRequest(srv.URL+"/target/blob.tar", http.MethodPut, "application/x-tar", map[string][]string{"X-Custom": {"hello"}})

	step := &v1alpha1.HTTPStreaming{
		Type: v1alpha1.HTTPStreamingV1alpha1,
		ID:   "upload",
		Spec: &v1alpha1.HTTPStreamingSpec{Resource: source, Request: request, TargetResource: target},
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

// TestHTTPStreamingTransformer_PublishedAccessDropsWriteVerb asserts the published
// download access on the output resource does not carry the upload write verb. The
// upload streams with PUT, but a later read (ocm download) must default to GET;
// re-issuing PUT would send an empty body and overwrite the uploaded object.
func TestHTTPStreamingTransformer_PublishedAccessDropsWriteVerb(t *testing.T) {
	r := require.New(t)
	payload := []byte("published access payload")
	var body []byte
	var method, header, contentType string

	out, err := runStreaming(t, payload, nil, &body, &method, &header, &contentType)
	r.NoError(err)
	assert.Equal(t, http.MethodPut, method, "the upload request must still use the configured PUT method")

	result := out.(*v1alpha1.HTTPStreaming)
	tw := wgetaccessv1.Wget{}
	r.NoError(wgetaccess.Scheme.Convert(result.Output.Resource.Access, &tw))
	assert.Empty(t, tw.Verb, "the published download access must not carry the upload write verb")
	assert.False(t, tw.NoRedirect, "the published download access must not carry upload-only request fields")
}

// TestHTTPStreamingTransformer_UploadErrorRedactsQueryToken asserts a transport-level
// upload failure does not leak the target URL's query token, which the wrapped
// *url.Error would otherwise expose.
func TestHTTPStreamingTransformer_UploadErrorRedactsQueryToken(t *testing.T) {
	r := require.New(t)
	scheme := newTransformerScheme()

	// A closed listener yields a connection-refused transport error from client.Do,
	// wrapped in a *url.Error carrying the full request URL including the query token.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	r.NoError(err)
	addr := ln.Addr().String()
	r.NoError(ln.Close())

	const token = "supersecrettoken"
	source := wgetResourceV2("blob", "https://source.example/blob.tar", "", "", nil, nil)
	target := wgetResourceV2("blob", "http://"+addr+"/target/blob.tar", "", "application/x-tar", nil, nil)
	request := wgetRequest("http://"+addr+"/target/blob.tar?access_token="+token, http.MethodPut, "application/x-tar", nil)

	step := &v1alpha1.HTTPStreaming{
		Type: v1alpha1.HTTPStreamingV1alpha1,
		ID:   "upload",
		Spec: &v1alpha1.HTTPStreamingSpec{Resource: source, Request: request, TargetResource: target},
	}
	tr := &HTTPStreamingTransformer{
		Scheme:             scheme,
		ResourceRepository: &stubResourceRepository{payload: []byte("x"), mediaType: "application/x-tar"},
	}

	_, err = tr.Transform(context.Background(), step)
	r.Error(err)
	assert.NotContains(t, err.Error(), token, "the upload error must not leak the target URL query token")
}

// TestHTTPStreamingTransformer_UserContentTypeHeaderPreserved asserts that an explicit
// Content-Type supplied via the request headers is not overridden by the media-type
// derived default.
func TestHTTPStreamingTransformer_UserContentTypeHeaderPreserved(t *testing.T) {
	r := require.New(t)
	scheme := newTransformerScheme()

	var gotContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotContentType = req.Header.Get("Content-Type")
		_, _ = io.ReadAll(req.Body)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	source := wgetResourceV2("blob", "https://source.example/blob.tar", "", "", nil, nil)
	target := wgetResourceV2("blob", srv.URL+"/target/blob.tar", "", "application/x-tar", nil, nil)
	// The request sets an explicit Content-Type that differs from the media type
	// (application/x-tar); it must reach the target unchanged.
	request := wgetRequest(srv.URL+"/target/blob.tar", http.MethodPut, "application/x-tar",
		map[string][]string{"Content-Type": {"application/vnd.custom+json"}})

	step := &v1alpha1.HTTPStreaming{
		Type: v1alpha1.HTTPStreamingV1alpha1,
		ID:   "upload",
		Spec: &v1alpha1.HTTPStreamingSpec{Resource: source, Request: request, TargetResource: target},
	}
	tr := &HTTPStreamingTransformer{
		Scheme:             scheme,
		ResourceRepository: &stubResourceRepository{payload: []byte("payload"), mediaType: "application/x-tar"},
	}

	_, err := tr.Transform(context.Background(), step)
	r.NoError(err)
	assert.Equal(t, "application/vnd.custom+json", gotContentType,
		"an explicit request Content-Type must not be overridden by the media-type default")
}

// runWithOpener streams through the "conv" opener (when selected by mutate) and returns the
// transform result and the bodies the target received.
func runWithOpener(t *testing.T, mutate func(*v1alpha1.HTTPStreamingSpec), conv SourceOpener) (runtime.Typed, [][]byte, error) {
	t.Helper()
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, body)
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(srv.Close)

	spec := &v1alpha1.HTTPStreamingSpec{
		Resource:       wgetResourceV2("blob", "https://source.example/blob.tar", "", "", nil, nil),
		Request:        wgetRequest(srv.URL+"/target/blob.tgz", http.MethodPut, "application/gzip", nil),
		TargetResource: wgetResourceV2("blob", srv.URL+"/target/blob.tgz", "", "application/gzip", nil, nil),
	}
	mutate(spec)
	tr := &HTTPStreamingTransformer{
		Scheme:             newTransformerScheme(),
		ResourceRepository: &stubResourceRepository{payload: []byte("raw"), mediaType: "application/x-tar"},
		Openers:            map[string]SourceOpener{"conv": conv},
	}
	out, err := tr.Transform(t.Context(), &v1alpha1.HTTPStreaming{Type: v1alpha1.HTTPStreamingV1alpha1, ID: "upload", Spec: spec})
	return out, bodies, err
}

func returning(payload string, derived bool) SourceOpener {
	return func(context.Context, SourceRequest) (OpenedSource, error) {
		return OpenedSource{Blob: inmemory.New(strings.NewReader(payload)), Derived: derived}, nil
	}
}

func withSource(opener string, digest *v2.Digest) func(*v1alpha1.HTTPStreamingSpec) {
	return func(s *v1alpha1.HTTPStreamingSpec) {
		s.Opener = opener
		s.Resource.Digest = digest
	}
}

func genericDigestOf(content string) *v2.Digest {
	return &v2.Digest{HashAlgorithm: hashAlgorithmSHA256, NormalisationAlgorithm: genericBlobDigestV1, Value: godigest.FromString(content).Encoded()}
}

func TestHTTPStreamingTransformer_OpenerReplacesDownload(t *testing.T) {
	r := require.New(t)
	out, bodies, err := runWithOpener(t, withSource("conv", &v2.Digest{
		HashAlgorithm:          hashAlgorithmSHA256,
		NormalisationAlgorithm: "ociArtifactDigest/v1",
		Value:                  "abc",
	}), returning("converted", false))
	r.NoError(err)
	r.Equal([][]byte{[]byte("converted")}, bodies, "the opener output, not the repository download, must be uploaded")
	r.Equal(genericDigestOf("converted"), out.(*v1alpha1.HTTPStreaming).Output.Resource.Digest,
		"a non-generic source digest describes another representation and is replaced by the uploaded digest")
}

func TestHTTPStreamingTransformer_UnknownOpener(t *testing.T) {
	r := require.New(t)
	_, bodies, err := runWithOpener(t, withSource("nope", nil), returning("", false))
	r.ErrorContains(err, `unknown source opener "nope"`)
	r.Empty(bodies, "an unknown opener must fail before any upload request")
}

func TestHTTPStreamingTransformer_OpenerDigest(t *testing.T) {
	tests := []struct {
		name    string
		derived bool
		wantErr string
	}{
		{name: "same representation verifies the generic source digest", wantErr: "digest mismatch"},
		{name: "derived representation replaces the generic source digest", derived: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			out, _, err := runWithOpener(t, withSource("conv", genericDigestOf("y")), returning("x", tt.derived))
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(genericDigestOf("x"), out.(*v1alpha1.HTTPStreaming).Output.Resource.Digest)
		})
	}
}

// stubLocalRepository serves one local resource of one component version.
type stubLocalRepository struct {
	repository.ComponentVersionRepository
	payload string
	got     []string
}

func (s *stubLocalRepository) GetLocalResource(_ context.Context, component, version string, identity runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	s.got = append(s.got, component+":"+version+" "+identity.String())
	return inmemory.New(strings.NewReader(s.payload)), nil, nil
}

type stubRepoProvider struct {
	repository.ComponentVersionRepositoryProvider
	repo *stubLocalRepository
	spec runtime.Typed
}

func (p *stubRepoProvider) GetComponentVersionRepository(_ context.Context, spec runtime.Typed, _ runtime.Typed) (repository.ComponentVersionRepository, error) {
	p.spec = spec
	return p.repo, nil
}

func TestHTTPStreamingTransformer_LocalResource(t *testing.T) {
	run := func(t *testing.T, opener string, conv SourceOpener) (*stubRepoProvider, [][]byte, error) {
		t.Helper()
		var bodies [][]byte
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			bodies = append(bodies, body)
			w.WriteHeader(http.StatusCreated)
		}))
		t.Cleanup(srv.Close)
		provider := &stubRepoProvider{repo: &stubLocalRepository{payload: "local"}}
		tr := &HTTPStreamingTransformer{
			Scheme:             newTransformerScheme(),
			ResourceRepository: &stubResourceRepository{payload: []byte("downloaded")},
			RepoProvider:       provider,
			Openers:            map[string]SourceOpener{"conv": conv},
		}
		_, err := tr.Transform(t.Context(), &v1alpha1.HTTPStreaming{Type: v1alpha1.HTTPStreamingV1alpha1, ID: "upload", Spec: &v1alpha1.HTTPStreamingSpec{
			Resource:       wgetResourceV2("blob", "https://source.example/blob.tar", "", "", nil, nil),
			Request:        wgetRequest(srv.URL+"/blob", http.MethodPut, "", nil),
			TargetResource: wgetResourceV2("blob", srv.URL+"/blob", "", "", nil, nil),
			Opener:         opener,
			ComponentVersion: &v1alpha1.SourceComponentVersion{
				Repository: &runtime.Raw{Type: runtime.NewVersionedType("CommonTransportFormat", "v1"), Data: []byte(`{"type":"CommonTransportFormat/v1","filePath":"/ctf"}`)},
				Component:  "ocm.software/c",
				Version:    "1.0.0",
			},
		}})
		return provider, bodies, err
	}

	t.Run("streamed from the source component version instead of downloading", func(t *testing.T) {
		r := require.New(t)
		provider, bodies, err := run(t, "", nil)
		r.NoError(err)
		r.Equal([][]byte{[]byte("local")}, bodies)
		r.Equal([]string{"ocm.software/c:1.0.0 name=blob,version=1.0.0"}, provider.repo.got)
		r.Equal("CommonTransportFormat/v1", provider.spec.GetType().String())
	})

	t.Run("handed to the opener without source credentials", func(t *testing.T) {
		r := require.New(t)
		var got SourceRequest
		_, bodies, err := run(t, "conv", func(ctx context.Context, src SourceRequest) (OpenedSource, error) {
			got = src
			b, _, err := src.Local.Repository.GetLocalResource(ctx, src.Local.Component, src.Local.Version, src.Resource.ToIdentity())
			return OpenedSource{Blob: b}, err
		})
		r.NoError(err)
		r.Equal([][]byte{[]byte("local")}, bodies)
		r.Nil(got.Credentials)
		r.Equal("ocm.software/c", got.Local.Component)
		r.NotNil(got.Target, "the opener sees the resource as it will be published")
	})
}

func TestHTTPStreamingTransformer_AfterUpload(t *testing.T) {
	tests := []struct {
		name          string
		reindexStatus int
		wantErr       string
	}{
		{name: "sent after the upload", reindexStatus: http.StatusOK},
		{name: "non-2xx fails the transformation", reindexStatus: http.StatusForbidden, wantErr: "after-upload request to"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			var calls []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				body, _ := io.ReadAll(req.Body)
				calls = append(calls, req.Method+" "+req.URL.Path+" "+string(body))
				if req.URL.Path == "/reindex" {
					w.WriteHeader(tt.reindexStatus)
					return
				}
				w.WriteHeader(http.StatusCreated)
			}))
			t.Cleanup(srv.Close)

			step := &v1alpha1.HTTPStreaming{
				Type: v1alpha1.HTTPStreamingV1alpha1,
				ID:   "upload",
				Spec: &v1alpha1.HTTPStreamingSpec{
					Resource:       wgetResourceV2("blob", "https://source.example/blob.tar", "", "", nil, nil),
					Request:        wgetRequest(srv.URL+"/upload", http.MethodPut, "", nil),
					TargetResource: wgetResourceV2("blob", srv.URL+"/upload", "", "", nil, nil),
					AfterUpload:    &wgetaccessv1.Wget{Type: wgetaccess.V1VersionedType, URL: srv.URL + "/reindex"},
				},
			}
			tr := &HTTPStreamingTransformer{
				Scheme:             newTransformerScheme(),
				ResourceRepository: &stubResourceRepository{payload: []byte("payload")},
			}
			_, err := tr.Transform(t.Context(), step)
			r.Equal([]string{"PUT /upload payload", "POST /reindex "}, calls, "the after-upload request is a body-less POST issued after the upload")
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
		})
	}
}
