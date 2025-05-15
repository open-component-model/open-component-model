package constructor_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"embed"
	_ "embed"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	"ocm.software/open-component-model/bindings/go/constructor"
	"ocm.software/open-component-model/bindings/go/constructor/input/helm/io"
	"ocm.software/open-component-model/bindings/go/constructor/spec"
	"ocm.software/open-component-model/bindings/go/ctf"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/oci"
	ocictf "ocm.software/open-component-model/bindings/go/oci/ctf"
	urlresolver "ocm.software/open-component-model/bindings/go/oci/resolver/url"
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
		ResourceRepositoryProvider: constructor.ResourceRepositoryProviderFn(func(ctx context.Context, resource *descruntime.Resource) (constructor.ResourceRepository, error) {
			// for the tests we direct our calls to a hardcoded repo, but in reality this would instantiate based on the access with the plugin manager
			// the plugin can introspect the resource, the access, then derive from that a repository
			resolver := urlresolver.New("ghcr.io")
			return oci.NewRepository(oci.WithResolver(resolver))
		}),
		ProcessByValue: func(resource *spec.Resource) bool {
			return resource.Name == "image"
		},
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

	r.Len(desc.Component.Resources, 5)

	t.Run("verify file blob", func(t *testing.T) {
		resource := desc.Component.Resources[0]
		r := require.New(t)
		b, _, err := repo.GetLocalResource(t.Context(), desc.Component.Name, desc.Component.Version, resource.ToIdentity())
		r.NoError(err)
		r.NotNil(b)

		var buf bytes.Buffer
		r.NoError(blob.Copy(&buf, b))

		expected, err := testData.ReadFile("testdata/text.txt")
		r.NoError(err)
		r.Equal(expected, buf.Bytes())
	})

	t.Run("verify (compressed) utf8 blob", func(t *testing.T) {
		resource := desc.Component.Resources[1]
		r := require.New(t)
		b, _, err := repo.GetLocalResource(t.Context(), desc.Component.Name, desc.Component.Version, resource.ToIdentity())
		r.NoError(err)
		r.NotNil(b)

		var buf bytes.Buffer
		r.NoError(blob.Copy(&buf, b))

		var zippedHW bytes.Buffer
		w := gzip.NewWriter(&zippedHW)
		_, err = w.Write([]byte("Hello, world!"))
		r.NoError(err)
		r.NoError(w.Close())

		r.Equal(zippedHW.Bytes(), buf.Bytes())
	})

	t.Run("verify utf8 JSON blob passed via object", func(t *testing.T) {
		resource := desc.Component.Resources[2]
		r := require.New(t)
		b, _, err := repo.GetLocalResource(t.Context(), desc.Component.Name, desc.Component.Version, resource.ToIdentity())
		r.NoError(err)
		r.NotNil(b)

		var buf bytes.Buffer
		r.NoError(blob.Copy(&buf, b))

		r.Equal("{\"key\":\"value\"}\n", buf.String())
	})

	t.Run("verify External OCI Image", func(t *testing.T) {
		resource := desc.Component.Resources[3]
		r := require.New(t)
		b, _, err := repo.GetLocalResource(t.Context(), desc.Component.Name, desc.Component.Version, resource.ToIdentity())
		r.NoErrorf(err, "the resource should have been constructed by value so it should be present")
		r.NotNil(b)

		layout, err := tar.ReadOCILayout(t.Context(), b)
		r.NoError(err)
		t.Cleanup(func() {
			r.NoError(layout.Close())
		})
		r.NotNil(layout)
	})

	t.Run("verify input based Helm Chart", func(t *testing.T) {
		resource := desc.Component.Resources[4]
		r := require.New(t)
		b, _, err := repo.GetLocalResource(t.Context(), desc.Component.Name, desc.Component.Version, resource.ToIdentity())
		r.NoError(err)
		r.NotNil(b)

		chart, err := io.ReadHelmOCILayout(t.Context(), b)
		r.NoError(err)
		r.NotNil(chart)
		r.Equal("chart", chart.Name())
		r.Equal("0.1.0", chart.Metadata.Version)
		r.NoError(chart.Validate())
	})
}
