package transformation_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	descriptor "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/helm/transformation"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// stubResourceRepo implements repository.ResourceRepository, returning a fixed blob from
// DownloadResource and errors from every other method.
type stubResourceRepo struct {
	blob blob.ReadOnlyBlob
}

func (s *stubResourceRepo) GetResourceCredentialConsumerIdentity(_ context.Context, _ *descriptor.Resource) (runtime.Identity, error) {
	return nil, errors.New("not implemented")
}

func (s *stubResourceRepo) UploadResource(_ context.Context, _ *descriptor.Resource, _ blob.ReadOnlyBlob, _ runtime.Typed) (*descriptor.Resource, error) {
	return nil, errors.New("not implemented")
}

func (s *stubResourceRepo) DownloadResource(_ context.Context, _ *descriptor.Resource, _ runtime.Typed) (blob.ReadOnlyBlob, error) {
	return s.blob, nil
}

// stubOCIRepo implements ocistream.ResourceRepository. DownloadResourceStream returns a
// pre-built OCIResourceStream; every other method returns an error.
type stubOCIRepo struct {
	stream ocistream.ResourceStream
}

func (s *stubOCIRepo) GetResourceCredentialConsumerIdentity(_ context.Context, _ *descriptor.Resource) (runtime.Identity, error) {
	return nil, errors.New("not implemented")
}

func (s *stubOCIRepo) UploadResource(_ context.Context, _ *descriptor.Resource, _ blob.ReadOnlyBlob, _ runtime.Typed) (*descriptor.Resource, error) {
	return nil, errors.New("not implemented")
}

func (s *stubOCIRepo) DownloadResource(_ context.Context, _ *descriptor.Resource, _ runtime.Typed) (blob.ReadOnlyBlob, error) {
	return nil, errors.New("not implemented")
}

func (s *stubOCIRepo) DownloadResourceStream(_ context.Context, _ *descriptor.Resource, _ runtime.Typed) (ocistream.ResourceStream, error) {
	return s.stream, nil
}

func (s *stubOCIRepo) UploadResourceStream(_ context.Context, _ *descriptor.Resource, _ ocistream.ResourceStream, _ runtime.Typed) (*descriptor.Resource, error) {
	return nil, errors.New("not implemented")
}

// pushBlob pushes content into a memory store and returns its descriptor.
func pushBlob(t *testing.T, ctx context.Context, store *memory.Store, mediaType string, content []byte) ocispec.Descriptor {
	t.Helper()
	desc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(content),
		Size:      int64(len(content)),
	}
	err := store.Push(ctx, desc, bytes.NewReader(content))
	require.NoError(t, err)
	return desc
}

// buildHelmTar creates a tar archive containing chartName (with chartData) and a provenance file.
func buildHelmTar(t *testing.T, chartName string, chartData []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	require.NoError(t, tw.WriteHeader(&tar.Header{Name: chartName, Size: int64(len(chartData))}))
	_, err := tw.Write(chartData)
	require.NoError(t, err)

	provData := []byte("provenance data")
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: chartName + ".prov", Size: int64(len(provData))}))
	_, err = tw.Write(provData)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	return buf.Bytes()
}

// buildOCIManifest builds a manifest with the given config and layers, pushes everything into
// store, and returns the manifest descriptor.
func buildOCIManifest(t *testing.T, ctx context.Context, store *memory.Store, configMediaType string, layers []ocispec.Descriptor) ocispec.Descriptor {
	t.Helper()
	configDesc := pushBlob(t, ctx, store, configMediaType, []byte("{}"))
	manifest := ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    layers,
	}
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	return pushBlob(t, ctx, store, ocispec.MediaTypeImageManifest, manifestBytes)
}

func TestChartArchiveSource_Open(t *testing.T) {
	t.Parallel()

	chartTGZ, err := os.ReadFile("../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)

	helmAccessRaw := &runtime.Raw{}
	require.NoError(t, helmAccessRaw.UnmarshalJSON([]byte(`{"type":"Helm/v1","helmRepository":"https://example.com","helmChart":"mychart:0.1.0"}`)))

	ociAccessRaw := &runtime.Raw{}
	require.NoError(t, ociAccessRaw.UnmarshalJSON([]byte(`{"type":"OCIImage/v1","imageReference":"registry.example.com/charts/mychart:0.1.0"}`)))

	wgetAccessRaw := &runtime.Raw{}
	require.NoError(t, wgetAccessRaw.UnmarshalJSON([]byte(`{"type":"Wget/v1","url":"https://example.com/mychart-0.1.0.tgz"}`)))

	t.Run("Helm/v1 extracts chart from tar", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		helmTarData := buildHelmTar(t, "mychart-0.1.0.tgz", chartTGZ)
		repo := &stubResourceRepo{blob: inmemory.New(bytes.NewReader(helmTarData))}
		source := &transformation.ChartArchiveSource{
			ResourceRepository: repo,
		}

		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "mychart", Version: "0.1.0"}},
			Type:        "helmChart",
			Access:      helmAccessRaw,
		}

		result, err := source.Open(t.Context(), resource, nil)
		r.NoError(err)

		rc, err := result.ReadCloser()
		r.NoError(err)
		defer rc.Close()

		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.Equal(chartTGZ, got)
	})

	t.Run("OCIImage/v1 streams chart layer", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()

		store := memory.New()
		chartContent := []byte("chart")
		chartDesc := pushBlob(t, ctx, store, registry.ChartLayerMediaType, chartContent)
		provDesc := pushBlob(t, ctx, store, "application/vnd.cncf.helm.provenance.v1.prov", []byte("prov"))
		manifestDesc := buildOCIManifest(t, ctx, store, "application/vnd.cncf.helm.config.v1+json", []ocispec.Descriptor{chartDesc, provDesc})

		ociRepo := &stubOCIRepo{
			stream: &ocistream.OCIResourceStream{ReadOnlyGraphStorage: store, Descriptor: manifestDesc},
		}
		source := &transformation.ChartArchiveSource{
			OCIRepository: ociRepo,
		}

		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "mychart", Version: "0.1.0"}},
			Type:        "helmChart",
			Access:      ociAccessRaw,
		}

		result, err := source.Open(ctx, resource, nil)
		r.NoError(err)

		rc, err := result.ReadCloser()
		r.NoError(err)
		defer rc.Close()

		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.Equal(chartContent, got)

		sizeAware, ok := result.(blob.SizeAware)
		r.True(ok, "returned blob should implement SizeAware")
		r.Equal(int64(5), sizeAware.Size())
	})

	t.Run("OCIImage/v1 with legacy media type", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()

		store := memory.New()
		chartContent := []byte("legacy-chart")
		chartDesc := pushBlob(t, ctx, store, "application/tar+gzip", chartContent)
		manifestDesc := buildOCIManifest(t, ctx, store, "application/vnd.cncf.helm.config.v1+json", []ocispec.Descriptor{chartDesc})

		ociRepo := &stubOCIRepo{
			stream: &ocistream.OCIResourceStream{ReadOnlyGraphStorage: store, Descriptor: manifestDesc},
		}
		source := &transformation.ChartArchiveSource{
			OCIRepository: ociRepo,
		}

		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "mychart", Version: "0.1.0"}},
			Type:        "helmChart",
			Access:      ociAccessRaw,
		}

		result, err := source.Open(ctx, resource, nil)
		r.NoError(err)

		rc, err := result.ReadCloser()
		r.NoError(err)
		defer rc.Close()

		got, err := io.ReadAll(rc)
		r.NoError(err)
		r.Equal(chartContent, got)
	})

	t.Run("OCIImage/v1 without chart layer", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)
		ctx := t.Context()

		store := memory.New()
		otherDesc := pushBlob(t, ctx, store, "application/vnd.oci.image.layer.v1.tar", []byte("not a chart"))
		manifestDesc := buildOCIManifest(t, ctx, store, "application/vnd.cncf.helm.config.v1+json", []ocispec.Descriptor{otherDesc})

		ociRepo := &stubOCIRepo{
			stream: &ocistream.OCIResourceStream{ReadOnlyGraphStorage: store, Descriptor: manifestDesc},
		}
		source := &transformation.ChartArchiveSource{
			OCIRepository: ociRepo,
		}

		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "mychart", Version: "0.1.0"}},
			Type:        "helmChart",
			Access:      ociAccessRaw,
		}

		_, err := source.Open(ctx, resource, nil)
		r.Error(err)
		r.Contains(err.Error(), "has no helm chart layer")
	})

	t.Run("Wget/v1 passes through unchanged", func(t *testing.T) {
		t.Parallel()
		r := require.New(t)

		passBlob := inmemory.New(bytes.NewReader(chartTGZ))
		repo := &stubResourceRepo{blob: passBlob}
		source := &transformation.ChartArchiveSource{
			ResourceRepository: repo,
		}

		resource := &descriptor.Resource{
			ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "mychart", Version: "0.1.0"}},
			Type:        "helmChart",
			Access:      wgetAccessRaw,
		}

		result, err := source.Open(t.Context(), resource, nil)
		r.NoError(err)

		// Assert exact same pointer — passthrough, not a copy.
		assert.Same(t, passBlob, result)
	})
}
