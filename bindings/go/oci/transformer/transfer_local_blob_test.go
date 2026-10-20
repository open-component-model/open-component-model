package transformer

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci/repository/provider"
	ctfspec "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/transformation/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestTransferLocalBlob_CTFToCTF streams a local blob between two CTF archives and reads
// it back from the target: the content arrives intact although the source blob can only
// be read once, and the source access's referenceName is kept.
func TestTransferLocalBlob_CTFToCTF(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	const component, version = "ocm.software/test", "1.0.0"
	content := []byte("streamed local blob content")

	ctfRepoSpec := func(path string) *runtime.Raw {
		raw := &runtime.Raw{}
		r.NoError(runtime.NewScheme(runtime.WithAllowUnknown()).Convert(&ctfspec.Repository{
			Type:       runtime.NewVersionedType(ctfspec.Type, ctfspec.Version),
			FilePath:   path,
			AccessMode: ctfspec.AccessModeReadWrite + "|" + ctfspec.AccessModeCreate,
		}, raw))
		return raw
	}
	sourceSpec, targetSpec := ctfRepoSpec(t.TempDir()), ctfRepoSpec(t.TempDir())
	repos := provider.NewComponentVersionRepositoryProvider()

	desc := &descriptor.Descriptor{
		Meta: descriptor.Meta{Version: "v2"},
		Component: descriptor.Component{
			ComponentMeta: descriptor.ComponentMeta{ObjectMeta: descriptor.ObjectMeta{Name: component, Version: version}},
			Provider:      descriptor.Provider{Name: "test-provider"},
		},
	}
	source, err := repos.GetComponentVersionRepository(ctx, sourceSpec, nil)
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
	out, err := (&TransferLocalBlob{Scheme: scheme, RepoProvider: repos}).Transform(ctx, &v1alpha1.TransferLocalBlob{
		Type: v1alpha1.TransferLocalBlobV1alpha1,
		Spec: &v1alpha1.TransferLocalBlobSpec{
			Source: v1alpha1.LocalBlobSource{
				Repository:       sourceSpec,
				Component:        component,
				Version:          version,
				ResourceIdentity: runtime.Identity{"name": "blob"},
			},
			Repository: targetSpec,
		},
	})
	r.NoError(err)
	transferred := descriptor.ConvertFromV2Resource(out.(*v1alpha1.TransferLocalBlob).Output.Resource)
	var access v2.LocalBlob
	r.NoError(v2.Scheme.Convert(transferred.Access, &access))
	r.Equal("org/blob:v1", access.ReferenceName)

	target, err := repos.GetComponentVersionRepository(ctx, targetSpec, nil)
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
