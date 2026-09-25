package chartarchive_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
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
	"ocm.software/open-component-model/bindings/go/helm/chartarchive"
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	ocistream "ocm.software/open-component-model/bindings/go/oci/stream"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// stubResourceRepo implements repository.ResourceRepository, returning a fixed blob from
// DownloadResource (or failing when blob is nil) and errors from every other method.
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
	if s.blob == nil {
		return nil, errors.New("unexpected download")
	}
	return s.blob, nil
}

// stubOCIRepo implements ocistream.ResourceRepository. DownloadResourceStream returns a
// pre-built stream; every other method returns an error.
type stubOCIRepo struct {
	stubResourceRepo
	stream ocistream.ResourceStream
}

func (s *stubOCIRepo) DownloadResourceStream(_ context.Context, _ *descriptor.Resource, _ runtime.Typed) (ocistream.ResourceStream, error) {
	return s.stream, nil
}

func (s *stubOCIRepo) UploadResourceStream(_ context.Context, _ *descriptor.Resource, _ ocistream.ResourceStream, _ runtime.Typed) (*descriptor.Resource, error) {
	return nil, errors.New("not implemented")
}

// localRepo serves a local blob through GetLocalResource only.
type localRepo struct {
	repository.ComponentVersionRepository
	blob blob.ReadOnlyBlob
}

func (l *localRepo) GetLocalResource(context.Context, string, string, runtime.Identity) (blob.ReadOnlyBlob, *descriptor.Resource, error) {
	return l.blob, nil, nil
}

// streamingLocalRepo serves a local blob as a lazy OCI stream, like OCI and CTF repositories.
type streamingLocalRepo struct {
	repository.ComponentVersionRepository
	stream ocistream.ResourceStream
}

func (l *streamingLocalRepo) GetLocalResourceStream(context.Context, string, string, runtime.Identity) (ocistream.ResourceStream, *descriptor.Resource, error) {
	return l.stream, nil, nil
}

func pushBlob(t *testing.T, ctx context.Context, store *memory.Store, mediaType string, content []byte) ocispec.Descriptor {
	t.Helper()
	desc := ocispec.Descriptor{MediaType: mediaType, Digest: digest.FromBytes(content), Size: int64(len(content))}
	require.NoError(t, store.Push(ctx, desc, bytes.NewReader(content)))
	return desc
}

// buildHelmTar creates the tar the helm downloader produces: the chart and a provenance file.
func buildHelmTar(t *testing.T, chartName string, chartData []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, f := range []struct {
		name string
		data []byte
	}{{chartName + ".prov", []byte("provenance data")}, {chartName, chartData}} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: f.name, Size: int64(len(f.data)), Mode: 0o644}))
		_, err := tw.Write(f.data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

// buildGzipTar creates a gzip-compressed tar with the given files.
func buildGzipTar(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, data := range files {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0o644}))
		_, err := io.WriteString(tw, data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())
	return buf.Bytes()
}

// buildOCIManifest pushes a manifest with the given config and layers into store and returns
// the manifest descriptor.
func buildOCIManifest(t *testing.T, ctx context.Context, store *memory.Store, configMediaType, config string, layers []ocispec.Descriptor) ocispec.Descriptor {
	t.Helper()
	configDesc := pushBlob(t, ctx, store, configMediaType, []byte(config))
	manifestBytes, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    configDesc,
		Layers:    layers,
	})
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
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "renamed", Version: "9.9.9"}},
		Type:        "helmChart",
		Access:      access,
	}
}

// helmRepoServer serves index.yaml and the chart of a classic Helm repository and counts
// chart downloads.
func helmRepoServer(t *testing.T, chart []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var downloads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.yaml":
			_, _ = io.WriteString(w, "apiVersion: v1\nentries:\n  mychart:\n  - name: mychart\n    version: 0.1.0\n    urls: [charts/mychart-0.1.0.tgz]\n")
		case "/charts/mychart-0.1.0.tgz":
			downloads.Add(1)
			_, _ = w.Write(chart)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &downloads
}

func TestSource_Open(t *testing.T) {
	chartTGZ, err := os.ReadFile("../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	const chartConfig = `{"name":"mychart","version":"0.1.0","apiVersion":"v2"}`

	helmSource := func(repoURL string) *descriptor.Resource {
		return resourceWith(rawAccess(t, `{"type":"Helm/v1","helmRepository":"`+repoURL+`","helmChart":"mychart:0.1.0"}`))
	}
	ociSource := resourceWith(rawAccess(t, `{"type":"OCIImage/v1","imageReference":"registry.example.com/charts/mychart:0.1.0"}`))
	wgetSource := resourceWith(rawAccess(t, `{"type":"Wget/v1","url":"https://example.com/mychart-0.1.0.tgz"}`))
	localBlob := func(mediaType string) *descriptor.Resource {
		return resourceWith(rawAccess(t, `{"type":"LocalBlob/v1","localReference":"sha256:abc","mediaType":"`+mediaType+`"}`))
	}
	// ociStore builds an OCI artifact; an empty layerMediaType omits the chart layer.
	ociStore := func(t *testing.T, configMediaType, layerMediaType string, layerContent []byte) (*memory.Store, ocispec.Descriptor) {
		ctx := t.Context()
		store := memory.New()
		layers := []ocispec.Descriptor{pushBlob(t, ctx, store, registry.ProvLayerMediaType, []byte("prov"))}
		if layerMediaType != "" {
			layers = append([]ocispec.Descriptor{pushBlob(t, ctx, store, layerMediaType, layerContent)}, layers...)
		}
		return store, buildOCIManifest(t, ctx, store, configMediaType, chartConfig, layers)
	}
	ociStream := func(t *testing.T, configMediaType, layerMediaType string, layerContent []byte) ocistream.ResourceStream {
		store, manifest := ociStore(t, configMediaType, layerMediaType, layerContent)
		return &ocistream.OCIResourceStream{ReadOnlyGraphStorage: store, Descriptor: manifest}
	}
	local := func(repo repository.ComponentVersionRepository) *chartarchive.Local {
		return &chartarchive.Local{Repository: repo, Component: "ocm.software/c", Version: "1.0.0"}
	}
	fromBytes := func(data []byte) blob.ReadOnlyBlob {
		return inmemory.New(bytes.NewReader(data), inmemory.WithSize(int64(len(data))))
	}

	type opened struct {
		source *chartarchive.Source
		req    chartarchive.Request
	}
	tests := []struct {
		name        string
		setup       func(t *testing.T) opened
		want        []byte
		wantFromOCI bool
		wantErr     string
	}{
		{
			name: "Helm/v1 streams the chart listed in the repository index",
			setup: func(t *testing.T) opened {
				srv, downloads := helmRepoServer(t, chartTGZ)
				t.Cleanup(func() { assert.Equal(t, int32(1), downloads.Load(), "the chart is fetched exactly once") })
				return opened{&chartarchive.Source{ResourceRepository: &stubResourceRepo{}}, chartarchive.Request{Resource: helmSource(srv.URL)}}
			},
			want: chartTGZ,
		},
		{
			name: "Helm/v1 with a keyring falls back to the helm downloader tar",
			setup: func(t *testing.T) opened {
				repo := &stubResourceRepo{blob: fromBytes(buildHelmTar(t, "mychart-0.1.0.tgz", chartTGZ))}
				creds := &helmcredsv1.HelmHTTPCredentials{Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), Keyring: "/keyring"}
				return opened{&chartarchive.Source{ResourceRepository: repo}, chartarchive.Request{Resource: helmSource("https://charts.example"), Credentials: creds}}
			},
			want: chartTGZ,
		},
		{
			name: "OCIImage/v1 streams the chart layer",
			setup: func(t *testing.T) opened {
				return opened{&chartarchive.Source{OCIRepository: &stubOCIRepo{stream: ociStream(t, registry.ConfigMediaType, registry.ChartLayerMediaType, []byte("chart"))}},
					chartarchive.Request{Resource: ociSource}}
			},
			want:        []byte("chart"),
			wantFromOCI: true,
		},
		{
			name: "OCIImage/v1 with legacy chart layer media type",
			setup: func(t *testing.T) opened {
				return opened{&chartarchive.Source{OCIRepository: &stubOCIRepo{stream: ociStream(t, registry.ConfigMediaType, registry.LegacyChartLayerMediaType, []byte("legacy"))}},
					chartarchive.Request{Resource: ociSource}}
			},
			want:        []byte("legacy"),
			wantFromOCI: true,
		},
		{
			name: "OCIImage/v1 without chart layer",
			setup: func(t *testing.T) opened {
				return opened{&chartarchive.Source{OCIRepository: &stubOCIRepo{stream: ociStream(t, registry.ConfigMediaType, "", nil)}}, chartarchive.Request{Resource: ociSource}}
			},
			wantErr: "has no helm chart layer",
		},
		{
			name: "OCIImage/v1 that is not a helm chart",
			setup: func(t *testing.T) opened {
				return opened{&chartarchive.Source{OCIRepository: &stubOCIRepo{stream: ociStream(t, ocispec.MediaTypeImageConfig, registry.ChartLayerMediaType, []byte("chart"))}},
					chartarchive.Request{Resource: ociSource}}
			},
			wantErr: `is not a helm chart: config media type "` + ocispec.MediaTypeImageConfig + `"`,
		},
		{
			name: "Wget/v1 serving a packaged chart yields it unchanged",
			setup: func(t *testing.T) opened {
				return opened{&chartarchive.Source{ResourceRepository: &stubResourceRepo{blob: fromBytes(chartTGZ)}}, chartarchive.Request{Resource: wgetSource}}
			},
			want: chartTGZ,
		},
		{
			name: "LocalBlob streamed as a plain layer",
			setup: func(t *testing.T) opened {
				store := memory.New()
				root := pushBlob(t, t.Context(), store, registry.ChartLayerMediaType, chartTGZ)
				repo := &streamingLocalRepo{stream: &ocistream.OCIResourceStream{ReadOnlyGraphStorage: store, Descriptor: root}}
				return opened{&chartarchive.Source{}, chartarchive.Request{Resource: localBlob(registry.ChartLayerMediaType), Local: local(repo)}}
			},
			want: chartTGZ,
		},
		{
			name: "LocalBlob streamed as an OCI manifest",
			setup: func(t *testing.T) opened {
				repo := &streamingLocalRepo{stream: ociStream(t, registry.ConfigMediaType, registry.ChartLayerMediaType, chartTGZ)}
				return opened{&chartarchive.Source{}, chartarchive.Request{Resource: localBlob(ocispec.MediaTypeImageManifest), Local: local(repo)}}
			},
			want:        chartTGZ,
			wantFromOCI: true,
		},
		{
			name: "LocalBlob from a non-streaming repository as an OCM OCI layout",
			setup: func(t *testing.T) opened {
				store, manifest := ociStore(t, registry.ConfigMediaType, registry.ChartLayerMediaType, chartTGZ)
				layoutBlob, err := ocitar.CopyToOCILayoutInMemory(t.Context(), store, manifest, ocitar.CopyToOCILayoutOptions{})
				require.NoError(t, err)
				return opened{&chartarchive.Source{}, chartarchive.Request{Resource: localBlob(ocispec.MediaTypeImageManifest), Local: local(&localRepo{blob: layoutBlob})}}
			},
			want:        chartTGZ,
			wantFromOCI: true,
		},
		{
			name: "LocalBlob content wins over its descriptor media type",
			setup: func(t *testing.T) opened {
				return opened{&chartarchive.Source{}, chartarchive.Request{Resource: localBlob("application/json"), Local: local(&localRepo{blob: fromBytes(chartTGZ)})}}
			},
			want: chartTGZ,
		},
		{
			name: "JSON content is not a chart",
			setup: func(t *testing.T) opened {
				return opened{&chartarchive.Source{}, chartarchive.Request{Resource: localBlob("application/json"), Local: local(&localRepo{blob: fromBytes([]byte(`{"not":"a chart"}`))})}}
			},
			wantErr: "is neither a packaged helm chart, a tar containing one, nor a helm chart OCI artifact",
		},
		{
			name: "gzip tar without Chart.yaml",
			setup: func(t *testing.T) opened {
				data := buildGzipTar(t, map[string]string{"mychart/values.yaml": "a: b\n"})
				return opened{&chartarchive.Source{ResourceRepository: &stubResourceRepo{blob: fromBytes(data)}}, chartarchive.Request{Resource: wgetSource}}
			},
			wantErr: "has no Chart.yaml within its first 1 MiB",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			o := tt.setup(t)
			got, err := o.source.Open(t.Context(), o.req)
			if tt.wantErr != "" {
				r.ErrorContains(err, tt.wantErr)
				return
			}
			r.NoError(err)
			r.Equal("mychart", got.Name)
			r.Equal("0.1.0", got.Version)
			r.Equal(tt.wantFromOCI, got.FromOCI)
			rc, err := got.Archive.ReadCloser()
			r.NoError(err)
			defer func() { _ = rc.Close() }()
			data, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(tt.want, data)
			sized, ok := got.Archive.(blob.SizeAware)
			r.True(ok, "the archive must report its size")
			if size := sized.Size(); size != blob.SizeUnknown {
				r.Equal(int64(len(tt.want)), size)
			}
		})
	}
}
