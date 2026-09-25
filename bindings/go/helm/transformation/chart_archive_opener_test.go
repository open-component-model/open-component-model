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
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
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

// buildOCIManifest pushes a helm chart manifest with the given config JSON and layers into
// store and returns the manifest descriptor.
func buildOCIManifest(t *testing.T, ctx context.Context, store *memory.Store, config string, layers []ocispec.Descriptor) ocispec.Descriptor {
	t.Helper()
	configDesc := pushBlob(t, ctx, store, registry.ConfigMediaType, []byte(config))
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

func rawAccess(t *testing.T, data string) *runtime.Raw {
	t.Helper()
	raw := &runtime.Raw{}
	require.NoError(t, raw.UnmarshalJSON([]byte(data)))
	return raw
}

func resourceWith(access *runtime.Raw) *descriptor.Resource {
	return &descriptor.Resource{
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "mychart", Version: "0.1.0"}},
		Type:        "helmChart",
		Access:      access,
	}
}

func TestChartArchiveSource_Open(t *testing.T) {
	chartTGZ, err := os.ReadFile("../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	const chartConfig = `{"name":"mychart","version":"0.1.0","apiVersion":"v2"}`

	target := func(helmChart string) *descriptor.Resource {
		return resourceWith(rawAccess(t, `{"type":"Helm/v1","helmRepository":"https://artifactory.example/artifactory/api/helm/r","helmChart":"`+helmChart+`"}`))
	}
	helmSource := resourceWith(rawAccess(t, `{"type":"Helm/v1","helmRepository":"https://example.com","helmChart":"mychart:0.1.0"}`))
	ociSource := resourceWith(rawAccess(t, `{"type":"OCIImage/v1","imageReference":"registry.example.com/charts/mychart:0.1.0"}`))
	localBlob := func(mediaType string) *descriptor.Resource {
		return resourceWith(rawAccess(t, `{"type":"LocalBlob/v1","localReference":"sha256:abc","mediaType":"`+mediaType+`"}`))
	}
	// ociStore builds a helm chart OCI artifact with the given chart layer media type and config.
	ociStore := func(t *testing.T, layerMediaType string, layerContent []byte, config string) (*memory.Store, ocispec.Descriptor) {
		ctx := t.Context()
		store := memory.New()
		layers := []ocispec.Descriptor{pushBlob(t, ctx, store, "application/vnd.cncf.helm.provenance.v1.prov", []byte("prov"))}
		if layerMediaType != "" {
			layers = append([]ocispec.Descriptor{pushBlob(t, ctx, store, layerMediaType, layerContent)}, layers...)
		}
		return store, buildOCIManifest(t, ctx, store, config, layers)
	}
	ociRepo := func(t *testing.T, layerMediaType string, layerContent []byte, config string) *stubOCIRepo {
		store, manifest := ociStore(t, layerMediaType, layerContent, config)
		return &stubOCIRepo{stream: &ocistream.OCIResourceStream{ReadOnlyGraphStorage: store, Descriptor: manifest}}
	}
	ociLayout := func(t *testing.T) blob.ReadOnlyBlob {
		store, manifest := ociStore(t, registry.ChartLayerMediaType, chartTGZ, chartConfig)
		b, err := ocitar.CopyToOCILayoutInMemory(t.Context(), store, manifest, ocitar.CopyToOCILayoutOptions{})
		require.NoError(t, err)
		return b
	}
	passBlob := inmemory.New(bytes.NewReader(chartTGZ))

	tests := []struct {
		name        string
		source      func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest)
		want        []byte
		wantSame    blob.ReadOnlyBlob
		wantDerived bool
		wantErr     string
	}{
		{
			name: "Helm/v1 extracts chart from the helm download",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				repo := &stubResourceRepo{blob: inmemory.New(bytes.NewReader(buildHelmTar(t, "mychart-0.1.0.tgz", chartTGZ)))}
				return &transformation.ChartArchiveSource{ResourceRepository: repo}, transformation.ChartRequest{Resource: helmSource, Target: target("mychart:0.1.0")}
			},
			want: chartTGZ,
		},
		{
			name: "Helm/v1 published under another name than Chart.yaml",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				repo := &stubResourceRepo{blob: inmemory.New(bytes.NewReader(buildHelmTar(t, "mychart-0.1.0.tgz", chartTGZ)))}
				return &transformation.ChartArchiveSource{ResourceRepository: repo}, transformation.ChartRequest{Resource: helmSource, Target: target("renamed:0.1.0")}
			},
			wantErr: "chart is mychart:0.1.0 according to its Chart.yaml, but it would be published as renamed:0.1.0",
		},
		{
			name: "OCIImage/v1 streams the chart layer",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				return &transformation.ChartArchiveSource{OCIRepository: ociRepo(t, registry.ChartLayerMediaType, []byte("chart"), chartConfig)},
					transformation.ChartRequest{Resource: ociSource, Target: target("mychart:0.1.0")}
			},
			want:        []byte("chart"),
			wantDerived: true,
		},
		{
			name: "OCIImage/v1 with legacy chart layer media type",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				return &transformation.ChartArchiveSource{OCIRepository: ociRepo(t, registry.LegacyChartLayerMediaType, []byte("legacy"), chartConfig)},
					transformation.ChartRequest{Resource: ociSource}
			},
			want:        []byte("legacy"),
			wantDerived: true,
		},
		{
			name: "OCIImage/v1 without chart layer",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				return &transformation.ChartArchiveSource{OCIRepository: ociRepo(t, "", nil, chartConfig)}, transformation.ChartRequest{Resource: ociSource}
			},
			wantErr: "has no helm chart layer",
		},
		{
			name: "OCIImage/v1 config version differs from the published version",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				return &transformation.ChartArchiveSource{OCIRepository: ociRepo(t, registry.ChartLayerMediaType, []byte("chart"), chartConfig)},
					transformation.ChartRequest{Resource: ociSource, Target: target("mychart:9.9.9")}
			},
			wantErr: "it would be published as mychart:9.9.9",
		},
		{
			name: "Wget/v1 passes through unchanged",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				wget := resourceWith(rawAccess(t, `{"type":"Wget/v1","url":"https://example.com/mychart-0.1.0.tgz"}`))
				return &transformation.ChartArchiveSource{ResourceRepository: &stubResourceRepo{blob: passBlob}}, transformation.ChartRequest{Resource: wget, Target: target("other:1.0.0")}
			},
			wantSame: passBlob,
		},
		{
			name: "LocalBlob packaged chart is uploaded as is",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				return &transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob(registry.ChartLayerMediaType), Target: target("mychart:0.1.0"), Buffered: passBlob}
			},
			wantSame: passBlob,
		},
		{
			name: "LocalBlob packaged chart published under another version",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				return &transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob("application/gzip"), Target: target("mychart:1.0.0"), Buffered: passBlob}
			},
			wantErr: "it would be published as mychart:1.0.0",
		},
		{
			name: "LocalBlob OCI layout yields its chart layer",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				return &transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob(layout.MediaTypeOCIImageLayoutTarGzipV1), Target: target("mychart:0.1.0"), Buffered: ociLayout(t)}
			},
			want:        chartTGZ,
			wantDerived: true,
		},
		{
			name: "LocalBlob stored as OCI manifest is read as the OCI layout the repository hands out",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				return &transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob(ocispec.MediaTypeImageManifest), Target: target("mychart:0.1.0"), Buffered: ociLayout(t)}
			},
			want:        chartTGZ,
			wantDerived: true,
		},
		{
			name: "LocalBlob with unsupported media type",
			source: func(t *testing.T) (*transformation.ChartArchiveSource, transformation.ChartRequest) {
				return &transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob("application/json"), Buffered: passBlob}
			},
			wantErr: `has media type "application/json", which is neither a packaged helm chart`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			source, req := tt.source(t)
			got, err := source.Open(t.Context(), req)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tt.wantDerived, got.Derived)
			if tt.wantSame != nil {
				assert.Same(t, tt.wantSame, got.Archive, "the source content must be passed through, not copied")
				return
			}
			rc, err := got.Archive.ReadCloser()
			r.NoError(err)
			defer func() { _ = rc.Close() }()
			data, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(tt.want, data)
			if sized, ok := got.Archive.(blob.SizeAware); ok {
				r.Equal(int64(len(tt.want)), sized.Size())
			}
		})
	}
}
