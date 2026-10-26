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

// TestTransferCTFLocalSourceToCTFLocalSource streams a local blob source between CTF archives
// and reads it back from the target: the content arrives intact and the output source keeps
// its local blob access.
func TestTransferCTFLocalSourceToCTFLocalSource(t *testing.T) {
	r := require.New(t)
	ctx := t.Context()
	const component, version = "ocm.software/test", "1.0.0"
	content := []byte("streamed local source content")

	ctfRepoSpec := func(path string) ctfspec.Repository {
		return ctfspec.Repository{
			Type:       runtime.NewVersionedType(ctfspec.Type, ctfspec.Version),
			FilePath:   path,
			AccessMode: ctfspec.AccessModeReadWrite + "|" + ctfspec.AccessModeCreate,
		}
	}
	sourceSpec := ctfRepoSpec(t.TempDir())
	targetSpec := ctfRepoSpec(t.TempDir())
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
	added, err := source.AddLocalSource(ctx, component, version, &descriptor.Source{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "src", Version: version}},
		Type:        "blob",
		Access: &v2.LocalBlob{
			Type:      runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
			MediaType: "text/plain",
		},
	}, inmemory.New(bytes.NewReader(content), inmemory.WithMediaType("text/plain")))
	r.NoError(err)
	desc.Component.Sources = []descriptor.Source{*added}
	r.NoError(source.AddComponentVersion(ctx, desc))

	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(v1alpha1.Scheme)
	out, err := (&TransferLocalSource{Scheme: scheme, RepoProvider: repos}).Transform(ctx, &v1alpha1.TransferCTFLocalSourceToCTFLocalSource{
		Type: v1alpha1.TransferCTFLocalSourceToCTFLocalSourceV1alpha1,
		Spec: &v1alpha1.TransferCTFLocalSourceToCTFLocalSourceSpec{
			Source: v1alpha1.CTFLocalSource{
				Repository:     sourceSpec,
				Component:      component,
				Version:        version,
				SourceIdentity: runtime.Identity{"name": "src"},
			},
			Repository: targetSpec,
		},
	})
	r.NoError(err)
	output := out.(*v1alpha1.TransferCTFLocalSourceToCTFLocalSource).Output
	r.NotNil(output)
	r.NotNil(output.Source)
	transferred := descriptor.ConvertFromV2Sources([]v2.Source{*output.Source})
	r.Len(transferred, 1)
	r.Equal("src", transferred[0].Name)
	r.True(v2.IsLocalBlob(transferred[0].Access), "transferred source must keep its local blob access")

	target, err := repos.GetComponentVersionRepository(ctx, &targetSpec, nil)
	r.NoError(err)
	desc.Component.Sources = transferred
	r.NoError(target.AddComponentVersion(ctx, desc))
	got, _, err := target.GetLocalSource(ctx, component, version, runtime.Identity{"name": "src"})
	r.NoError(err)
	rc, err := got.ReadCloser()
	r.NoError(err)
	t.Cleanup(func() { _ = rc.Close() })
	data, err := io.ReadAll(rc)
	r.NoError(err)
	r.Equal(content, data)
}

func TestTransferLocalSource_RequiresSpec(t *testing.T) {
	r := require.New(t)
	scheme := runtime.NewScheme()
	scheme.MustRegisterScheme(v1alpha1.Scheme)
	_, err := (&TransferLocalSource{Scheme: scheme}).Transform(t.Context(), &v1alpha1.TransferCTFLocalSourceToCTFLocalSource{
		Type: v1alpha1.TransferCTFLocalSourceToCTFLocalSourceV1alpha1,
	})
	r.ErrorContains(err, "spec is required")
}
