package transformation_test

import (
	"archive/tar"
	"bytes"
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
	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
	"ocm.software/open-component-model/bindings/go/helm/transformation"
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
	for name, data := range map[string][]byte{chartName: chartData, chartName + ".prov": []byte("provenance data")} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data))}))
		_, err := tw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return buf.Bytes()
}

// buildOCIManifest pushes a helm chart manifest with the given config JSON and layers into
// store and returns the manifest descriptor.
func buildOCIManifest(t *testing.T, ctx context.Context, store *memory.Store, config string, layers []ocispec.Descriptor) ocispec.Descriptor {
	t.Helper()
	configDesc := pushBlob(t, ctx, store, registry.ConfigMediaType, []byte(config))
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
		ElementMeta: descriptor.ElementMeta{ObjectMeta: descriptor.ObjectMeta{Name: "mychart", Version: "0.1.0"}},
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

func TestChartArchiveSource_Open(t *testing.T) {
	chartTGZ, err := os.ReadFile("../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	const chartConfig = `{"name":"mychart","version":"0.1.0","apiVersion":"v2"}`

	target := func(helmChart string) *descriptor.Resource {
		return resourceWith(rawAccess(t, `{"type":"Helm/v1","helmRepository":"https://artifactory.example/artifactory/api/helm/r","helmChart":"`+helmChart+`"}`))
	}
	helmSource := func(repoURL string) *descriptor.Resource {
		return resourceWith(rawAccess(t, `{"type":"Helm/v1","helmRepository":"`+repoURL+`","helmChart":"mychart:0.1.0"}`))
	}
	ociSource := resourceWith(rawAccess(t, `{"type":"OCIImage/v1","imageReference":"registry.example.com/charts/mychart:0.1.0"}`))
	localBlob := func(mediaType string) *descriptor.Resource {
		return resourceWith(rawAccess(t, `{"type":"LocalBlob/v1","localReference":"sha256:abc","mediaType":"`+mediaType+`"}`))
	}
	// ociStore builds a helm chart OCI artifact; an empty layerMediaType omits the chart layer.
	ociStore := func(t *testing.T, layerMediaType string, layerContent []byte) (*memory.Store, ocispec.Descriptor) {
		ctx := t.Context()
		store := memory.New()
		layers := []ocispec.Descriptor{pushBlob(t, ctx, store, registry.ProvLayerMediaType, []byte("prov"))}
		if layerMediaType != "" {
			layers = append([]ocispec.Descriptor{pushBlob(t, ctx, store, layerMediaType, layerContent)}, layers...)
		}
		return store, buildOCIManifest(t, ctx, store, chartConfig, layers)
	}
	ociStream := func(t *testing.T, layerMediaType string, layerContent []byte) ocistream.ResourceStream {
		store, manifest := ociStore(t, layerMediaType, layerContent)
		return &ocistream.OCIResourceStream{ReadOnlyGraphStorage: store, Descriptor: manifest}
	}
	local := func(repo repository.ComponentVersionRepository) *transformation.LocalSource {
		return &transformation.LocalSource{Repository: repo, Component: "ocm.software/c", Version: "1.0.0"}
	}
	passBlob := inmemory.New(bytes.NewReader(chartTGZ))

	type opened struct {
		source *transformation.ChartArchiveSource
		req    transformation.ChartRequest
	}
	tests := []struct {
		name        string
		setup       func(t *testing.T) opened
		want        []byte
		wantSame    blob.ReadOnlyBlob
		wantDerived bool
		wantErr     string
	}{
		{
			name: "Helm/v1 streams the chart listed in the repository index",
			setup: func(t *testing.T) opened {
				srv, downloads := helmRepoServer(t, chartTGZ)
				t.Cleanup(func() { assert.Equal(t, int32(1), downloads.Load(), "the chart is fetched exactly once") })
				return opened{&transformation.ChartArchiveSource{ResourceRepository: &stubResourceRepo{}}, transformation.ChartRequest{Resource: helmSource(srv.URL), Target: target("mychart:0.1.0")}}
			},
			want: chartTGZ,
		},
		{
			name: "Helm/v1 published under another name than Chart.yaml",
			setup: func(t *testing.T) opened {
				srv, _ := helmRepoServer(t, chartTGZ)
				return opened{&transformation.ChartArchiveSource{ResourceRepository: &stubResourceRepo{}}, transformation.ChartRequest{Resource: helmSource(srv.URL), Target: target("renamed:0.1.0")}}
			},
			wantErr: "chart is mychart:0.1.0 according to its Chart.yaml, but it would be published as renamed:0.1.0",
		},
		{
			name: "Helm/v1 with a keyring falls back to the helm downloader",
			setup: func(t *testing.T) opened {
				repo := &stubResourceRepo{blob: inmemory.New(bytes.NewReader(buildHelmTar(t, "mychart-0.1.0.tgz", chartTGZ)))}
				creds := &helmcredsv1.HelmHTTPCredentials{Type: runtime.NewVersionedType(helmcredsv1.HelmHTTPCredentialsType, helmcredsv1.Version), Keyring: "/keyring"}
				return opened{&transformation.ChartArchiveSource{ResourceRepository: repo}, transformation.ChartRequest{Resource: helmSource("https://charts.example"), Target: target("mychart:0.1.0"), Credentials: creds}}
			},
			want: chartTGZ,
		},
		{
			name: "OCIImage/v1 streams the chart layer",
			setup: func(t *testing.T) opened {
				return opened{&transformation.ChartArchiveSource{OCIRepository: &stubOCIRepo{stream: ociStream(t, registry.ChartLayerMediaType, []byte("chart"))}},
					transformation.ChartRequest{Resource: ociSource, Target: target("mychart:0.1.0")}}
			},
			want:        []byte("chart"),
			wantDerived: true,
		},
		{
			name: "OCIImage/v1 with legacy chart layer media type",
			setup: func(t *testing.T) opened {
				return opened{&transformation.ChartArchiveSource{OCIRepository: &stubOCIRepo{stream: ociStream(t, registry.LegacyChartLayerMediaType, []byte("legacy"))}},
					transformation.ChartRequest{Resource: ociSource}}
			},
			want:        []byte("legacy"),
			wantDerived: true,
		},
		{
			name: "OCIImage/v1 without chart layer",
			setup: func(t *testing.T) opened {
				return opened{&transformation.ChartArchiveSource{OCIRepository: &stubOCIRepo{stream: ociStream(t, "", nil)}}, transformation.ChartRequest{Resource: ociSource}}
			},
			wantErr: "has no helm chart layer",
		},
		{
			name: "OCIImage/v1 config version differs from the published version",
			setup: func(t *testing.T) opened {
				return opened{&transformation.ChartArchiveSource{OCIRepository: &stubOCIRepo{stream: ociStream(t, registry.ChartLayerMediaType, []byte("chart"))}},
					transformation.ChartRequest{Resource: ociSource, Target: target("mychart:9.9.9")}}
			},
			wantErr: "it would be published as mychart:9.9.9",
		},
		{
			name: "Wget/v1 passes through unchanged",
			setup: func(t *testing.T) opened {
				wget := resourceWith(rawAccess(t, `{"type":"Wget/v1","url":"https://example.com/mychart-0.1.0.tgz"}`))
				return opened{&transformation.ChartArchiveSource{ResourceRepository: &stubResourceRepo{blob: passBlob}}, transformation.ChartRequest{Resource: wget}}
			},
			wantSame: passBlob,
		},
		{
			name: "Wget/v1 chart is checked against the published name",
			setup: func(t *testing.T) opened {
				wget := resourceWith(rawAccess(t, `{"type":"Wget/v1","url":"https://example.com/mychart-0.1.0.tgz"}`))
				return opened{&transformation.ChartArchiveSource{ResourceRepository: &stubResourceRepo{blob: inmemory.New(bytes.NewReader(chartTGZ))}},
					transformation.ChartRequest{Resource: wget, Target: target("other:0.1.0")}}
			},
			wantErr: "it would be published as other:0.1.0",
		},
		{
			name: "LocalBlob packaged chart streams from the component version",
			setup: func(t *testing.T) opened {
				store := memory.New()
				root := pushBlob(t, t.Context(), store, registry.ChartLayerMediaType, chartTGZ)
				repo := &streamingLocalRepo{stream: &ocistream.OCIResourceStream{ReadOnlyGraphStorage: store, Descriptor: root}}
				return opened{&transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob(registry.ChartLayerMediaType), Target: target("mychart:0.1.0"), Local: local(repo)}}
			},
			want: chartTGZ,
		},
		{
			name: "LocalBlob packaged chart published under another version",
			setup: func(t *testing.T) opened {
				store := memory.New()
				root := pushBlob(t, t.Context(), store, "application/gzip", chartTGZ)
				repo := &streamingLocalRepo{stream: &ocistream.OCIResourceStream{ReadOnlyGraphStorage: store, Descriptor: root}}
				return opened{&transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob("application/gzip"), Target: target("mychart:1.0.0"), Local: local(repo)}}
			},
			wantErr: "it would be published as mychart:1.0.0",
		},
		{
			name: "LocalBlob OCI artifact streams its chart layer from the component version",
			setup: func(t *testing.T) opened {
				repo := &streamingLocalRepo{stream: ociStream(t, registry.ChartLayerMediaType, chartTGZ)}
				return opened{&transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob(ocispec.MediaTypeImageManifest), Target: target("mychart:0.1.0"), Local: local(repo)}}
			},
			want:        chartTGZ,
			wantDerived: true,
		},
		{
			name: "LocalBlob from a repository without streaming access: packaged chart",
			setup: func(t *testing.T) opened {
				return opened{&transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob(registry.ChartLayerMediaType), Target: target("mychart:0.1.0"), Local: local(&localRepo{blob: inmemory.New(bytes.NewReader(chartTGZ))})}}
			},
			want: chartTGZ,
		},
		{
			name: "LocalBlob from a repository without streaming access: OCI layout",
			setup: func(t *testing.T) opened {
				store, manifest := ociStore(t, registry.ChartLayerMediaType, chartTGZ)
				layoutBlob, err := ocitar.CopyToOCILayoutInMemory(t.Context(), store, manifest, ocitar.CopyToOCILayoutOptions{})
				require.NoError(t, err)
				return opened{&transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob(ocispec.MediaTypeImageManifest), Target: target("mychart:0.1.0"), Local: local(&localRepo{blob: layoutBlob})}}
			},
			want:        chartTGZ,
			wantDerived: true,
		},
		{
			name: "LocalBlob with unsupported media type",
			setup: func(t *testing.T) opened {
				return opened{&transformation.ChartArchiveSource{}, transformation.ChartRequest{Resource: localBlob("application/json"), Local: local(&localRepo{blob: passBlob})}}
			},
			wantErr: `has media type "application/json", which is neither a packaged helm chart`,
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
			if sized, ok := got.Archive.(blob.SizeAware); ok && sized.Size() != blob.SizeUnknown {
				r.Equal(int64(len(tt.want)), sized.Size())
			}
		})
	}
}
