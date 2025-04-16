package constructor_test

import (
	"embed"
	_ "embed"
	"io"
	"os"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	"ocm.software/open-component-model/bindings/go/ctf"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

//go:embed testdata
var testData embed.FS

func TestConstruct(t *testing.T) {
	r := require.New(t)
	data, err := testData.ReadFile("testdata/component-constructor.01.yaml")
	r.NoError(err)
	r.NotEmpty(data)
	var constructorSpec spec.ComponentConstructor
	r.NoError(yaml.Unmarshal(data, &constructorSpec))

	fs, err := filesystem.NewFS(t.TempDir(), os.O_RDWR)
	r.NoError(err)
	store := ocictf.NewFromCTF(ctf.NewFileSystemCTF(fs))
	repo, err := oci.NewRepository(ocictf.WithCTF(store))
	r.NoError(err)

	descs, err := constructor.Construct(t.Context(), &constructorSpec, constructor.Options{
		Target: repo,
	})
	r.NoError(err)
	r.Len(descs, 1)

	desc := descs[0]

	v2desc, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
	r.NoError(err)
	r.NoError(v2.Validate(v2desc))

	r.Len(desc.Component.Resources, 1)

	resource := desc.Component.Resources[0]

	b, err := repo.DownloadResource(t.Context(), &resource)
	r.NoError(err)
	r.NotNil(b)

	layout, err := tar.ReadOCILayout(t.Context(), b)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(layout.Close())
	})
	r.NotNil(layout)
	r.Len(layout.Index.Manifests, 1)

	manifest := layout.Index.Manifests[0]
	manifestDataStream, err := layout.Fetch(t.Context(), manifest)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(manifestDataStream.Close())
	})
	manifestData, err := io.ReadAll(manifestDataStream)
	r.NoError(err)

	var manifestDesc ociImageSpecV1.Manifest
	r.NoError(yaml.Unmarshal(manifestData, &manifestDesc))
	r.NotEmpty(manifestDesc)
	r.Len(manifestDesc.Layers, 1)

	layer := manifestDesc.Layers[0]
	layerDataStream, err := layout.Fetch(t.Context(), layer)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(layerDataStream.Close())
	})

	data, err = io.ReadAll(layerDataStream)
	r.NoError(err)
	r.NotEmpty(data)

	expected, err := testData.ReadFile("testdata/text.txt")
	r.NoError(err)
	r.Equal(expected, data)
}
