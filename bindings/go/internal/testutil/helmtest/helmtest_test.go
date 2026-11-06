package helmtest_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/opencontainers/go-digest"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"

	"ocm.software/open-component-model/bindings/go/internal/testutil/helmtest"
)

func TestChartRegistry(t *testing.T) {
	tgz := []byte("chart archive")
	prov := []byte("provenance")

	tests := []struct {
		name       string
		opts       []helmtest.Option
		wantLayers map[string][]byte
	}{
		{
			name:       "chart only",
			wantLayers: map[string][]byte{registry.ChartLayerMediaType: tgz},
		},
		{
			name:       "with provenance",
			opts:       []helmtest.Option{helmtest.WithProvenance(prov)},
			wantLayers: map[string][]byte{registry.ChartLayerMediaType: tgz, registry.ProvLayerMediaType: prov},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			reg := helmtest.ChartRegistry(t, "charts/mychart", "0.1.0", tgz, tt.opts...)
			base := reg.URL + "/v2/charts/mychart/"

			status, _ := fetch(t, reg, http.MethodGet, reg.URL+"/v2/")
			r.Equal(http.StatusOK, status)

			status, byTag := fetch(t, reg, http.MethodGet, base+"manifests/0.1.0")
			r.Equal(http.StatusOK, status)
			r.Equal(reg.Manifest, digest.FromBytes(byTag))
			status, byDigest := fetch(t, reg, http.MethodGet, base+"manifests/"+reg.Manifest.String())
			r.Equal(http.StatusOK, status)
			r.Equal(byTag, byDigest)

			var manifest ociImageSpecV1.Manifest
			r.NoError(json.Unmarshal(byTag, &manifest))
			r.Equal(registry.ConfigMediaType, manifest.Config.MediaType)
			status, config := fetch(t, reg, http.MethodGet, base+"blobs/"+manifest.Config.Digest.String())
			r.Equal(http.StatusOK, status)
			r.Equal(manifest.Config.Digest, digest.FromBytes(config))
			r.JSONEq(helmtest.Config, string(config))

			r.Len(manifest.Layers, len(tt.wantLayers))
			for _, layer := range manifest.Layers {
				want, ok := tt.wantLayers[layer.MediaType]
				r.True(ok, "unexpected layer %s", layer.MediaType)
				r.Equal(digest.FromBytes(want), layer.Digest)
				status, data := fetch(t, reg, http.MethodGet, base+"blobs/"+layer.Digest.String())
				r.Equal(http.StatusOK, status)
				r.Equal(want, data)
			}

			status, head := fetch(t, reg, http.MethodHead, base+"blobs/"+digest.FromBytes(tgz).String())
			r.Equal(http.StatusOK, status)
			r.Empty(head)

			status, _ = fetch(t, reg, http.MethodGet, base+"manifests/0.2.0")
			r.Equal(http.StatusNotFound, status)
			status, _ = fetch(t, reg, http.MethodGet, reg.URL+"/v2/charts/other/manifests/0.1.0")
			r.Equal(http.StatusNotFound, status)
		})
	}
}

func fetch(t *testing.T, reg *helmtest.Registry, method, url string) (int, []byte) {
	t.Helper()
	r := require.New(t)
	req, err := http.NewRequestWithContext(t.Context(), method, url, nil)
	r.NoError(err)
	resp, err := reg.Client().Do(req)
	r.NoError(err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	r.NoError(err)
	return resp.StatusCode, body
}
