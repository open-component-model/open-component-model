package transformer

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	_ "crypto/sha256"

	godigest "github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/ctf"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// fakeLazyBlob is a size-unknown, media-type-aware blob that is neither
// blob.SizeAware nor blob.DigestAware, mirroring a lazy streaming source blob.
type fakeLazyBlob struct {
	data      string
	mediaType string
}

var _ blob.MediaTypeAware = (*fakeLazyBlob)(nil)

func (b *fakeLazyBlob) ReadCloser() (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(b.data)), nil
}

func (b *fakeLazyBlob) MediaType() (string, bool) {
	if b.mediaType == "" {
		return "", false
	}
	return b.mediaType, true
}

// fakeStreamingResourceRepo implements repository.StreamingResourceRepository and
// records which download method was used.
type fakeStreamingResourceRepo struct {
	blob           blob.ReadOnlyBlob
	materialized   blob.ReadOnlyBlob
	streamCalled   bool
	downloadCalled bool
}

var _ repository.StreamingResourceRepository = (*fakeStreamingResourceRepo)(nil)

func (f *fakeStreamingResourceRepo) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (f *fakeStreamingResourceRepo) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return res, nil
}

func (f *fakeStreamingResourceRepo) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	f.downloadCalled = true
	if f.materialized != nil {
		return f.materialized, nil
	}
	return f.blob, nil
}

func (f *fakeStreamingResourceRepo) DownloadResourceStream(ctx context.Context, res *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	f.streamCalled = true
	return f.blob, nil
}

// primingLazyBlob mirrors a wget/s3 lazy source: its media type (the response
// Content-Type) is only known once its stream is opened. It is idempotent, so
// the fused transformer may open it to prime the media type.
type primingLazyBlob struct {
	data        string
	contentType string
	primed      bool
	opens       int
}

var (
	_ blob.MediaTypeAware         = (*primingLazyBlob)(nil)
	_ repository.IdempotentSource = (*primingLazyBlob)(nil)
)

func (b *primingLazyBlob) ReadCloser() (io.ReadCloser, error) {
	b.primed = true
	b.opens++
	return io.NopCloser(strings.NewReader(b.data)), nil
}

func (b *primingLazyBlob) MediaType() (string, bool) {
	if !b.primed || b.contentType == "" {
		return "", false
	}
	return b.contentType, true
}

func (b *primingLazyBlob) Idempotent() bool { return true }

// nonIdempotentStreamBlob mirrors a source whose request must not be repeated
// (for example a wget access with a non-GET verb and a body). It records every
// open so a test can assert the streaming path never issued the request.
type nonIdempotentStreamBlob struct {
	opens int
}

var (
	_ blob.MediaTypeAware         = (*nonIdempotentStreamBlob)(nil)
	_ repository.IdempotentSource = (*nonIdempotentStreamBlob)(nil)
)

func (b *nonIdempotentStreamBlob) ReadCloser() (io.ReadCloser, error) {
	b.opens++
	return io.NopCloser(strings.NewReader("")), nil
}

func (b *nonIdempotentStreamBlob) MediaType() (string, bool) { return "", false }

func (b *nonIdempotentStreamBlob) Idempotent() bool { return false }

// fakeResourceRepo implements only repository.ResourceRepository (no streaming).
type fakeResourceRepo struct {
	blob           blob.ReadOnlyBlob
	downloadCalled bool
}

var _ repository.ResourceRepository = (*fakeResourceRepo)(nil)

func (f *fakeResourceRepo) GetResourceCredentialConsumerIdentity(ctx context.Context, resource *descriptor.Resource) (runtime.Identity, error) {
	return nil, nil
}

func (f *fakeResourceRepo) UploadResource(ctx context.Context, res *descriptor.Resource, content blob.ReadOnlyBlob, credentials runtime.Typed) (*descriptor.Resource, error) {
	return res, nil
}

func (f *fakeResourceRepo) DownloadResource(ctx context.Context, res *descriptor.Resource, credentials runtime.Typed) (blob.ReadOnlyBlob, error) {
	f.downloadCalled = true
	return f.blob, nil
}

func streamTransformerScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	v2.MustAddToScheme(s)
	s.MustRegisterWithAlias(&v1alpha1.OCIStreamLocalResource{}, v1alpha1.OCIStreamLocalResourceV1alpha1)
	return s
}

func wgetSourceResource(name string, digest *v2.Digest) *v2.Resource {
	return &v2.Resource{
		ElementMeta: v2.ElementMeta{
			ObjectMeta: v2.ObjectMeta{Name: name, Version: "1.0.0"},
		},
		Type:     "blob",
		Relation: v2.ExternalRelation,
		Access: &runtime.Raw{
			Type: runtime.NewVersionedType("Wget", "v1"),
			Data: []byte(`{"type":"Wget/v1","url":"https://example.com/a.bin"}`),
		},
		Digest: digest,
	}
}

// TestStreamLocalResource_StreamingPath verifies the fused transformer opens the
// source as a lazy, size-unknown stream and reaches AddLocalResource with that
// blob, deriving a LocalBlob access with the source media type.
func TestStreamLocalResource_StreamingPath(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	lazy := &fakeLazyBlob{data: "streamed content", mediaType: "application/x-test"}
	resourceRepo := &fakeStreamingResourceRepo{blob: lazy}
	mockRepo := &mockRepository{}
	provider := &mockRepoProvider{repo: mockRepo}

	tr := &StreamLocalResource{
		Scheme:             streamTransformerScheme(t),
		RepoProvider:       provider,
		ResourceRepository: resourceRepo,
	}

	spec := &v1alpha1.OCIStreamLocalResource{
		Type: v1alpha1.OCIStreamLocalResourceV1alpha1,
		ID:   "stream",
		Spec: &v1alpha1.OCIStreamLocalResourceSpec{
			Repository: ocispec.Repository{
				Type:    runtime.Type{Name: ocispec.Type, Version: "v1"},
				BaseUrl: "ghcr.io/test",
			},
			Component: "ocm.software/test",
			Version:   "1.0.0",
			Resource:  wgetSourceResource("blob", nil),
		},
	}

	result, err := tr.Transform(ctx, spec)
	r.NoError(err)
	r.NotNil(result)

	r.True(resourceRepo.streamCalled, "expected the streaming download to be used")
	r.False(resourceRepo.downloadCalled, "the buffered download must not be used on the streaming path")

	// The blob handed to AddLocalResource must have unknown size so pack routes it
	// to the chunked streaming push.
	r.NotNil(mockRepo.addedBlob)
	if sizer, ok := mockRepo.addedBlob.(blob.SizeAware); ok {
		r.Equal(blob.SizeUnknown, sizer.Size(), "streaming blob must report an unknown size")
	}

	// The derived access is a v2.LocalBlob carrying the source media type. It MUST
	// be the descriptor/v2 type (not the typed runtime.LocalBlob) so pack can
	// convert it with the OCI repository scheme.
	localBlob, ok := mockRepo.addedResource.Access.(*v2.LocalBlob)
	r.True(ok, "expected a *v2.LocalBlob access, got %T", mockRepo.addedResource.Access)
	r.Equal("application/x-test", localBlob.MediaType)
	r.Equal("blob", localBlob.ReferenceName)
	r.Equal(v2.LocalBlobAccessType, localBlob.Type.Name)
	r.Equal(v2.LocalBlobAccessTypeVersion, localBlob.Type.Version)

	transformed := result.(*v1alpha1.OCIStreamLocalResource)
	r.NotNil(transformed.Output)
	r.NotNil(transformed.Output.Resource)
	r.Equal(v2.LocalBlobAccessType, transformed.Output.Resource.Access.GetType().Name)
}

// TestStreamLocalResource_DigestWrapping verifies that a SHA-256 source digest is
// exposed on the streamed blob so the chunked push can verify against it.
func TestStreamLocalResource_DigestWrapping(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	lazy := &fakeLazyBlob{data: "streamed content", mediaType: "application/x-test"}
	resourceRepo := &fakeStreamingResourceRepo{blob: lazy}
	mockRepo := &mockRepository{}
	provider := &mockRepoProvider{repo: mockRepo}

	tr := &StreamLocalResource{
		Scheme:             streamTransformerScheme(t),
		RepoProvider:       provider,
		ResourceRepository: resourceRepo,
	}

	hexValue := strings.Repeat("a", 64)
	spec := &v1alpha1.OCIStreamLocalResource{
		Type: v1alpha1.OCIStreamLocalResourceV1alpha1,
		ID:   "stream",
		Spec: &v1alpha1.OCIStreamLocalResourceSpec{
			Repository: ocispec.Repository{
				Type:    runtime.Type{Name: ocispec.Type, Version: "v1"},
				BaseUrl: "ghcr.io/test",
			},
			Component: "ocm.software/test",
			Version:   "1.0.0",
			Resource: wgetSourceResource("blob", &v2.Digest{
				HashAlgorithm:          "SHA-256",
				NormalisationAlgorithm: "genericBlobDigest/v1",
				Value:                  hexValue,
			}),
		},
	}

	_, err := tr.Transform(ctx, spec)
	r.NoError(err)

	r.NotNil(mockRepo.addedBlob)
	digestAware, ok := mockRepo.addedBlob.(blob.DigestAware)
	r.True(ok, "expected the streamed blob to expose a known digest")
	dig, known := digestAware.Digest()
	r.True(known)
	r.Equal("sha256:"+hexValue, dig)
	// Size must still be unknown so streaming engages.
	if sizer, ok := mockRepo.addedBlob.(blob.SizeAware); ok {
		r.Equal(blob.SizeUnknown, sizer.Size())
	}
}

// TestStreamLocalResource_FallbackPath verifies that a non-streaming resource
// repository falls back to the buffered DownloadResource and still reaches
// AddLocalResource.
func TestStreamLocalResource_FallbackPath(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	lazy := &fakeLazyBlob{data: "buffered content", mediaType: "application/x-test"}
	resourceRepo := &fakeResourceRepo{blob: lazy}
	mockRepo := &mockRepository{}
	provider := &mockRepoProvider{repo: mockRepo}

	tr := &StreamLocalResource{
		Scheme:             streamTransformerScheme(t),
		RepoProvider:       provider,
		ResourceRepository: resourceRepo,
	}

	spec := &v1alpha1.OCIStreamLocalResource{
		Type: v1alpha1.OCIStreamLocalResourceV1alpha1,
		ID:   "stream",
		Spec: &v1alpha1.OCIStreamLocalResourceSpec{
			Repository: ocispec.Repository{
				Type:    runtime.Type{Name: ocispec.Type, Version: "v1"},
				BaseUrl: "ghcr.io/test",
			},
			Component: "ocm.software/test",
			Version:   "1.0.0",
			Resource:  wgetSourceResource("blob", nil),
		},
	}

	result, err := tr.Transform(ctx, spec)
	r.NoError(err)
	r.NotNil(result)

	r.True(resourceRepo.downloadCalled, "the buffered download must be used in the fallback path")
	r.NotNil(mockRepo.addedResource)
	r.Equal("ocm.software/test", mockRepo.component)
}

// ociStreamSpec builds an OCIStreamLocalResource step for the given source
// resource, reducing boilerplate across the media-type tests.
func ociStreamSpec(src *v2.Resource) *v1alpha1.OCIStreamLocalResource {
	return &v1alpha1.OCIStreamLocalResource{
		Type: v1alpha1.OCIStreamLocalResourceV1alpha1,
		ID:   "stream",
		Spec: &v1alpha1.OCIStreamLocalResourceSpec{
			Repository: ocispec.Repository{
				Type:    runtime.Type{Name: ocispec.Type, Version: "v1"},
				BaseUrl: "ghcr.io/test",
			},
			Component: "ocm.software/test",
			Version:   "1.0.0",
			Resource:  src,
		},
	}
}

// TestStreamLocalResource_UntypedSourceMediaType verifies that a wget/s3 source
// with NO pinned media type still packs: the response Content-Type is primed from
// the stream when the server sends one, and application/octet-stream is used as
// the fallback when it does not. This guards the media-type regression where an
// untyped source derived an empty LocalBlob media type and pack rejected it.
func TestStreamLocalResource_UntypedSourceMediaType(t *testing.T) {
	tests := []struct {
		name          string
		contentType   string
		wantMediaType string
	}{
		{name: "server sends content type", contentType: "text/plain", wantMediaType: "text/plain"},
		{name: "server sends none falls back to octet-stream", contentType: "", wantMediaType: "application/octet-stream"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			ctx := context.Background()

			lazy := &primingLazyBlob{data: "streamed content", contentType: tc.contentType}
			resourceRepo := &fakeStreamingResourceRepo{blob: lazy}
			mockRepo := &mockRepository{}
			provider := &mockRepoProvider{repo: mockRepo}

			tr := &StreamLocalResource{
				Scheme:             streamTransformerScheme(t),
				RepoProvider:       provider,
				ResourceRepository: resourceRepo,
			}

			result, err := tr.Transform(ctx, ociStreamSpec(wgetSourceResource("blob", nil)))
			r.NoError(err)
			r.NotNil(result)

			r.True(resourceRepo.streamCalled, "the streaming download must be used for an idempotent source")

			localBlob, ok := mockRepo.addedResource.Access.(*v2.LocalBlob)
			r.True(ok, "expected a *v2.LocalBlob access, got %T", mockRepo.addedResource.Access)
			r.Equal(tc.wantMediaType, localBlob.MediaType)
			r.NotEmpty(localBlob.MediaType, "an untyped source must never derive an empty media type")

			// Size must stay unknown so pack routes the blob to the streaming push.
			if sizer, ok := mockRepo.addedBlob.(blob.SizeAware); ok {
				r.Equal(blob.SizeUnknown, sizer.Size())
			}
		})
	}
}

// TestStreamLocalResource_MaterializesNonIdempotentStream guards the idempotency
// gate on the fused path: a streaming source whose request is not idempotent must
// be materialized via DownloadResource and must never be opened by the streaming
// path, so the non-idempotent request is issued only once. Mirrors the uploader's
// TestSourceMaterializesNonIdempotentStream.
func TestStreamLocalResource_MaterializesNonIdempotentStream(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	stream := &nonIdempotentStreamBlob{}
	materialized := &fakeLazyBlob{data: "materialized", mediaType: "text/plain"}
	resourceRepo := &fakeStreamingResourceRepo{blob: stream, materialized: materialized}
	mockRepo := &mockRepository{}
	provider := &mockRepoProvider{repo: mockRepo}

	tr := &StreamLocalResource{
		Scheme:             streamTransformerScheme(t),
		RepoProvider:       provider,
		ResourceRepository: resourceRepo,
	}

	result, err := tr.Transform(ctx, ociStreamSpec(wgetSourceResource("blob", nil)))
	r.NoError(err)
	r.NotNil(result)

	r.Equal(0, stream.opens, "the non-idempotent streaming source must never be opened")
	r.True(resourceRepo.downloadCalled, "the non-idempotent source must be materialized via DownloadResource")
	r.Equal(materialized, mockRepo.addedBlob, "the materialized blob must be the one added")

	localBlob, ok := mockRepo.addedResource.Access.(*v2.LocalBlob)
	r.True(ok, "expected a *v2.LocalBlob access, got %T", mockRepo.addedResource.Access)
	r.Equal("text/plain", localBlob.MediaType)
}

// newInProcessOCIRepository builds a real *oci.Repository backed by an in-process
// CTF store (a tempdir directory, no network or Docker). It stands in for an OCI
// registry so the real pack conversion and local-blob storage/retrieval run
// against an actual *oci.Repository without any external dependency.
func newInProcessOCIRepository(t *testing.T) *oci.Repository {
	t.Helper()
	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	require.NoError(t, err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(ocictf.WithCTF(store), oci.WithTempDir(t.TempDir()))
	require.NoError(t, err)
	return repo
}

// ociRepoProvider is a stub [repository.ComponentVersionRepositoryProvider] that
// always returns the same in-process *oci.Repository.
type ociRepoProvider struct {
	repo *oci.Repository
}

func (p *ociRepoProvider) GetComponentVersionRepositoryCredentialConsumerIdentity(context.Context, runtime.Typed) (runtime.Identity, error) {
	return nil, nil
}

func (p *ociRepoProvider) GetComponentVersionRepository(context.Context, runtime.Typed, runtime.Typed) (repository.ComponentVersionRepository, error) {
	return p.repo, nil
}

func (p *ociRepoProvider) GetJSONSchemaForRepositorySpecification(runtime.Type) ([]byte, error) {
	return nil, nil
}

// TestStreamLocalResource_RealPackOCI exercises the REAL pack path (not a stubbed
// repository) against an actual in-process OCI *oci.Repository, so the access
// handed to repo.AddLocalResource -> pack.ArtifactBlob is converted for real.
// This guards the regression where the transformer set a typed runtime.LocalBlob
// access that pack could not convert to v2.LocalBlob ("cannot assign value of
// type *runtime.LocalBlob to target of type *v2.LocalBlob"). The source stays a
// simple in-memory lazy stub; only the target repository is real.
func TestStreamLocalResource_RealPackOCI(t *testing.T) {
	r := require.New(t)
	ctx := context.Background()

	const content = "real pack streaming content"
	lazy := &primingLazyBlob{data: content, contentType: "application/x-test"}
	resourceRepo := &fakeStreamingResourceRepo{blob: lazy}

	repo := newInProcessOCIRepository(t)

	tr := &StreamLocalResource{
		Scheme:             streamTransformerScheme(t),
		RepoProvider:       &ociRepoProvider{repo: repo},
		ResourceRepository: resourceRepo,
	}

	spec := &v1alpha1.OCIStreamLocalResource{
		Type: v1alpha1.OCIStreamLocalResourceV1alpha1,
		ID:   "stream",
		Spec: &v1alpha1.OCIStreamLocalResourceSpec{
			Repository: ocispec.Repository{
				Type:    runtime.Type{Name: ocispec.Type, Version: "v1"},
				BaseUrl: "ghcr.io/test",
			},
			Component: "ocm.software/test",
			Version:   "1.0.0",
			Resource:  wgetSourceResource("blob", nil),
		},
	}

	result, err := tr.Transform(ctx, spec)
	r.NoError(err) // before the fix this failed inside pack converting the access
	r.NotNil(result)

	out, ok := result.(*v1alpha1.OCIStreamLocalResource)
	r.True(ok)
	r.NotNil(out.Output)
	r.NotNil(out.Output.Resource)
	r.True(resourceRepo.streamCalled, "an idempotent source must take the streaming path")
	r.NotNil(out.Output.Resource.Access, "the transferred resource must carry an access")
	r.Contains(strings.ToLower(out.Output.Resource.Access.GetType().Name), "localblob",
		"the transferred resource must end up with a LocalBlob access")

	// The blob must be retrievable from the real OCI repository with the expected
	// content digest. The push wrote the blob layer and pinned the LocalBlob
	// LocalReference to its digest; creating the component version makes the
	// resource (and thus the blob) resolvable through GetLocalResource.
	convRes := descriptor.ConvertFromV2Resource(out.Output.Resource)
	r.NotNil(convRes)
	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{
				ObjectMeta: descriptor.ObjectMeta{Name: "ocm.software/test", Version: "1.0.0"},
			},
			Provider:  descriptor.Provider{Name: "test"},
			Resources: []descriptor.Resource{*convRes},
		},
	}
	r.NoError(repo.AddComponentVersion(ctx, desc))

	got, _, err := repo.GetLocalResource(ctx, "ocm.software/test", "1.0.0", convRes.ToIdentity())
	r.NoError(err)
	rc, err := got.ReadCloser()
	r.NoError(err)
	defer rc.Close()
	gotBytes, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, string(gotBytes))
	r.Equal(godigest.FromString(content).String(), godigest.FromBytes(gotBytes).String(),
		"the retrieved blob must have the expected content digest")
}
