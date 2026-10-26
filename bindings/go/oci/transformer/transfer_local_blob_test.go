package transformer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	ociblob "ocm.software/open-component-model/bindings/go/oci/blob"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestTransferCTFLocalBlobToCTFLocalBlob streams a local blob between CTF archives and
// reads it back from the target: the content arrives intact although the source blob can
// only be read once, and the source access's referenceName is kept. Within a single
// archive the open source stream must not block the write to the target.
func TestTransferCTFLocalBlobToCTFLocalBlob(t *testing.T) {
	for _, tc := range []struct {
		name        string
		sameArchive bool
	}{
		{name: "separate archives"},
		{name: "same archive", sameArchive: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testTransferCTFLocalBlobToCTFLocalBlob(t, tc.sameArchive)
		})
	}
}

func testTransferCTFLocalBlobToCTFLocalBlob(t *testing.T, sameArchive bool) {
	t.Helper()
	r := require.New(t)
	ctx := t.Context()
	const component, version = "ocm.software/test", "1.0.0"
	content := []byte("streamed local blob content")

	ctfRepoSpec := func(path string) ctfspec.Repository {
		return ctfspec.Repository{
			Type:       runtime.NewVersionedType(ctfspec.Type, ctfspec.Version),
			FilePath:   path,
			AccessMode: ctfspec.AccessModeReadWrite + "|" + ctfspec.AccessModeCreate,
		}
	}
	sourceSpec := ctfRepoSpec(t.TempDir())
	targetSpec := sourceSpec
	if !sameArchive {
		targetSpec = ctfRepoSpec(t.TempDir())
	}
	repos := provider.NewComponentVersionRepositoryProvider()

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version}},
			Provider:      descriptor.Provider{Name: "test-provider"},
		},
	}
	source, err := repos.GetComponentVersionRepository(ctx, &sourceSpec, nil)
	r.NoError(err)
	added, err := source.AddLocalResource(ctx, component, version, &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "blob", Version: version}},
		Type:        "plainText",
		Relation:    descriptor.LocalRelation,
		Access: &v2.LocalBlob{
			Type:          runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			MediaType:     "text/plain",
			ReferenceName: "org/blob:v1",
		},
	}, inmemory.New(bytes.NewReader(content), inmemory.WithMediaType("text/plain")))
	r.NoError(err)
	desc.Component.Resources = []descriptor.Resource{*added}
	r.NoError(source.AddComponentVersion(ctx, desc))

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(v1alpha1.Scheme)
	out, err := (&TransferLocalBlob{Scheme: scheme, RepoProvider: repos}).Transform(ctx, &v1alpha1.TransferCTFLocalBlobToCTFLocalBlob{
		Type: v1alpha1.TransferCTFLocalBlobToCTFLocalBlobV1alpha1,
		Spec: &v1alpha1.TransferCTFLocalBlobToCTFLocalBlobSpec{
			Source: v1alpha1.CTFLocalBlobSource{
				Repository:       sourceSpec,
				Component:        component,
				Version:          version,
				ResourceIdentity: runtime.Identity{"name": "blob"},
			},
			Repository: targetSpec,
		},
	})
	r.NoError(err)
	transferred := descriptor.ConvertFromV2Resource(out.(*v1alpha1.TransferCTFLocalBlobToCTFLocalBlob).Output.Resource)
	var access v2.LocalBlob
	r.NoError(v2.Scheme.Convert(transferred.Access, &access))
	r.Equal("org/blob:v1", access.ReferenceName)

	target, err := repos.GetComponentVersionRepository(ctx, &targetSpec, nil)
	r.NoError(err)
	desc.Component.Resources = []descriptor.Resource{*transferred}
	r.NoError(target.AddComponentVersion(ctx, desc))
	got, _, err := target.GetLocalResource(ctx, component, version, runtime.Identity{"name": "blob"})
	r.NoError(err)
	rc, err := got.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { _ = rc.Close() })
	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, data)
}

type trackingReader struct {
	io.Reader
	closed bool
}

func (r *trackingReader) Close() error { r.closed = true; return nil }

// failingTargetRepository serves a fixed local blob and rejects every add before reading it.
type failingTargetRepository struct {
	repository.ComponentVersionRepository
	content  blob.ReadOnlyBlob
	resource *descriptor.Resource
}

func (r *failingTargetRepository) GetLocalResource(context.Context, string, string, runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return r.content, r.resource, nil
}

func (r *failingTargetRepository) AddLocalResource(context.Context, string, string, *descriptor.Resource, blob.ReadOnlyBlob) (*descriptor.Resource, error) {
	return nil, errors.New("target unavailable")
}

type staticRepositoryProvider struct {
	repository.ComponentVersionRepositoryProvider
	repo repository.ComponentVersionRepository
}

func (p *staticRepositoryProvider) GetComponentVersionRepository(context.Context, runtime.Typed, runtime.Typed) (repository.ComponentVersionRepository, error) {
	return p.repo, nil
}

// TestTransferLocalBlob_ReleasesSourceOnTargetFailure checks that no source stream is left
// open when the target fails before consuming it.
func TestTransferLocalBlob_ReleasesSourceOnTargetFailure(t *testing.T) {
	r := require.New(t)
	data := []byte("source body")
	var source *trackingReader
	open := func() (io.ReadCloser, error) {
		source = &trackingReader{Reader: bytes.NewReader(data)}
		return source, nil
	}
	d := digest.FromBytes(data)
	repo := &failingTargetRepository{
		content: ociblob.NewDescriptorBlob(open, ocispec.Descriptor{Digest: d, Size: int64(len(data)), MediaType: "text/plain"}),
		resource: &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "payload", Version: "1.0.0"}},
			Type:        "plainText",
			Relation:    descriptor.LocalRelation,
			Access: &v2.LocalBlob{
				Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
				LocalReference: d.String(),
				MediaType:      "text/plain",
			},
		},
	}
	spec := ctfspec.Repository{Type: runtime.NewVersionedType(ctfspec.Type, ctfspec.Version), FilePath: t.TempDir()}
	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(v1alpha1.Scheme)

	_, err := (&TransferLocalBlob{Scheme: scheme, RepoProvider: &staticRepositoryProvider{repo: repo}}).Transform(t.Context(), &v1alpha1.TransferCTFLocalBlobToCTFLocalBlob{
		Type: v1alpha1.TransferCTFLocalBlobToCTFLocalBlobV1alpha1,
		Spec: &v1alpha1.TransferCTFLocalBlobToCTFLocalBlobSpec{
			Source: v1alpha1.CTFLocalBlobSource{
				Repository:       spec,
				Component:        "example.org/c",
				Version:          "1.0.0",
				ResourceIdentity: runtime.Identity{"name": "payload"},
			},
			Repository: spec,
		},
	})
	r.ErrorContains(err, "target unavailable")
	r.True(source == nil || source.closed, "a source stream opened for a target that fails before reading it must be closed")
}
