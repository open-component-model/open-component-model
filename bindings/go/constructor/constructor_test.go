package constructor_test

import (
	"bytes"
	"embed"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"testing"

	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v3/pkg/chart/loader"
	registry2 "helm.sh/helm/v3/pkg/registry"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor"
	"ocm.software/open-component-model/bindings/go/constructor/input/helm"
	helmspec "ocm.software/open-component-model/bindings/go/constructor/input/helm/spec"
	"ocm.software/open-component-model/bindings/go/ctf"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	"ocm.software/open-component-model/bindings/go/oci/tar"

	v1 "ocm.software/open-component-model/bindings/go/constructor/input/helm/spec/v1"
	"ocm.software/open-component-model/bindings/go/constructor/input/registry"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

//go:embed testdata
var testData embed.FS

func init() {
	helmspec.MustAddToScheme(registry.Scheme)
	registry.Default.MustRegisterMethod(&v1.Helm{}, &helm.Method{Scheme: registry.Scheme})
}

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
	// resolver := url.New("ghcr.io/jakobmoellerdev/constructor-test")
	// resolver.SetClient(&auth.Client{Credential: auth.StaticCredential("ghcr.io", auth.Credential{Username: "foobar", Password: "foobar"})})
	// repo, err := oci.NewRepository(oci.WithResolver(resolver))
	// r.NoError(err)

	descs, err := constructor.Construct(t.Context(), &constructorSpec, constructor.Options{
		Target: repo,
	})
	r.NoError(err)
	r.Len(descs, 1)

	desc := descs[0]

	v2desc, err := descruntime.ConvertToV2(runtime.NewScheme(runtime.WithAllowUnknown()), desc)
	r.NoError(err)
	r.NoError(v2.Validate(v2desc))

	descYAML, err := yaml.Marshal(v2desc)
	r.NoError(err)
	r.NotEmpty(descYAML)
	t.Log(string(descYAML))

	r.Len(desc.Component.Resources, 4)

	resource := desc.Component.Resources[0]

	b, _, err := repo.GetLocalResource(t.Context(), desc.Component.Name, desc.Component.Version, resource.ToIdentity())
	r.NoError(err)
	r.NotNil(b)

	var buf bytes.Buffer
	r.NoError(blob.Copy(&buf, b))

	expected, err := testData.ReadFile("testdata/text.txt")
	r.NoError(err)
	r.Equal(expected, buf.Bytes())

	resource = desc.Component.Resources[3]
	b, _, err = repo.GetLocalResource(t.Context(), desc.Component.Name, desc.Component.Version, resource.ToIdentity())
	r.NoError(err)
	r.NotNil(b)

	layout, err := tar.ReadOCILayout(t.Context(), b)
	t.Cleanup(func() {
		r.NoError(layout.Close())
	})
	r.NoError(err)
	r.NotNil(layout)

	r.Len(layout.Index.Manifests, 1)

	manifestDesc := layout.Index.Manifests[0]
	manifestDataStream, err := layout.Fetch(t.Context(), manifestDesc)
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(manifestDataStream.Close())
	})
	manifestData, err := io.ReadAll(manifestDataStream)
	r.NoError(err)
	var manifest ociImageSpecV1.Manifest
	r.NoError(json.Unmarshal(manifestData, &manifest))

	r.Len(manifest.Layers, 1)
	r.Equal(registry2.ChartLayerMediaType, manifest.Layers[0].MediaType)

	chartStream, err := layout.Fetch(t.Context(), manifest.Layers[0])
	r.NoError(err)
	t.Cleanup(func() {
		r.NoError(chartStream.Close())
	})

	chart, err := loader.LoadArchive(chartStream)
	r.NoError(err)
	r.NotNil(chart)
	r.Equal("chart", chart.Metadata.Name)
	r.Equal(resource.Version, chart.Metadata.Version)
}
