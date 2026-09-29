package chart

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"
	"oras.land/oras-go/v2/content/memory"

	"ocm.software/open-component-model/bindings/go/blob"
	"ocm.software/open-component-model/bindings/go/blob/inmemory"
	"ocm.software/open-component-model/bindings/go/oci/spec/layout"
	ocitar "ocm.software/open-component-model/bindings/go/oci/tar"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// chartLayout returns the OCI layout (as GetLocalResource and DownloadResource hand out OCI
// artifacts) of a helm chart OCI artifact with the config media type and chart.
func chartLayout(t *testing.T, configMediaType string, chart []byte) blob.ReadOnlyBlob {
	t.Helper()
	ctx := t.Context()
	store := memory.New()
	push := func(mediaType string, data []byte) ocispec.Descriptor {
		desc := ocispec.Descriptor{MediaType: mediaType, Digest: digest.FromBytes(data), Size: int64(len(data))}
		require.NoError(t, store.Push(ctx, desc, bytes.NewReader(data)))
		return desc
	}
	manifest, err := json.Marshal(ocispec.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ocispec.MediaTypeImageManifest,
		Config:    push(configMediaType, []byte(`{"name":"mychart","version":"0.1.0","apiVersion":"v2"}`)),
		Layers:    []ocispec.Descriptor{push(registry.ChartLayerMediaType, chart), push(registry.ProvLayerMediaType, []byte("prov"))},
	})
	require.NoError(t, err)
	b, err := ocitar.CopyToOCILayoutInMemory(ctx, store, push(ocispec.MediaTypeImageManifest, manifest), ocitar.CopyToOCILayoutOptions{})
	require.NoError(t, err)
	return b
}

func TestLocate(t *testing.T) {
	chartTGZ, err := os.ReadFile("../../../../helm/testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	var helmTar bytes.Buffer
	tw := tar.NewWriter(&helmTar)
	for _, f := range []struct {
		name string
		data []byte
	}{{"mychart-0.1.0.tgz.prov", []byte("provenance")}, {"mychart-0.1.0.tgz", chartTGZ}} {
		require.NoError(t, tw.WriteHeader(&tar.Header{Name: f.name, Size: int64(len(f.data)), Mode: 0o644}))
		_, err := tw.Write(f.data)
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	fromBytes := func(data []byte) blob.ReadOnlyBlob {
		return inmemory.New(bytes.NewReader(data), inmemory.WithSize(int64(len(data))))
	}

	tests := []struct {
		name        string
		content     func(t *testing.T) blob.ReadOnlyBlob
		mediaType   string
		want        []byte
		wantFromOCI bool
		wantErr     string
	}{
		{
			name:    "packaged chart as is",
			content: func(*testing.T) blob.ReadOnlyBlob { return fromBytes(chartTGZ) },
			want:    chartTGZ,
		},
		{
			name:    "first .tgz of a helm downloader tar",
			content: func(*testing.T) blob.ReadOnlyBlob { return fromBytes(helmTar.Bytes()) },
			want:    chartTGZ,
		},
		{
			name:        "chart layer of a helm chart OCI layout",
			content:     func(t *testing.T) blob.ReadOnlyBlob { return chartLayout(t, registry.ConfigMediaType, chartTGZ) },
			mediaType:   layout.MediaTypeOCIImageLayoutTarGzipV1,
			want:        chartTGZ,
			wantFromOCI: true,
		},
		{
			name:      "OCI layout of an artifact that is no helm chart",
			content:   func(t *testing.T) blob.ReadOnlyBlob { return chartLayout(t, ocispec.MediaTypeImageConfig, chartTGZ) },
			mediaType: layout.MediaTypeOCIImageLayoutTarGzipV1,
			wantErr:   `is not a helm chart: config media type "` + ocispec.MediaTypeImageConfig + `"`,
		},
		{
			name:    "content that is no chart",
			content: func(*testing.T) blob.ReadOnlyBlob { return fromBytes([]byte("not a chart")) },
			wantErr: "is neither a packaged helm chart, a tar containing one, nor a helm chart OCI artifact",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			chart, fromOCI, err := Locate(t.Context(), tc.content(t), tc.mediaType, runtime.Identity{"name": "mychart"})
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantFromOCI, fromOCI)
			rc, err := chart.ReadCloser()
			r.NoError(err)
			defer func() { _ = rc.Close() }()
			got, err := io.ReadAll(rc)
			r.NoError(err)
			r.Equal(tc.want, got)
			if tc.wantFromOCI {
				d, known := chart.(blob.DigestAware).Digest()
				r.True(known, "the chart layer digest is known up front")
				r.Equal(digest.FromBytes(chartTGZ).String(), d)
			}
		})
	}
}
