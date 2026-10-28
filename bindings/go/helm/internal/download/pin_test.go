package download

import (
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"
)

func TestPinOCIReference(t *testing.T) {
	pin := godigest.FromString("manifest")
	tests := []struct {
		name    string
		ref     string
		want    string
		wantErr string
	}{
		{name: "tag is replaced by the digest", ref: "oci://localhost:5000/a/b/chart:1.0", want: "oci://localhost:5000/a/b/chart@" + pin.String()},
		{name: "tag and digest are replaced by the pin", ref: "oci://localhost:5000/a/b/chart:1.0@" + godigest.FromString("other").String(), want: "oci://localhost:5000/a/b/chart@" + pin.String()},
		{name: "reference without tag", ref: "oci://registry.example.com/charts/chart", want: "oci://registry.example.com/charts/chart@" + pin.String()},
		{name: "non-oci reference", ref: "https://registry.example.com/charts/chart:1.0", wantErr: "a manifest digest pins only oci:// chart references"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			got, err := pinOCIReference(tc.ref, pin)
			if tc.wantErr != "" {
				r.ErrorContains(err, tc.wantErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.want, got)
		})
	}
}

func TestRenameChart(t *testing.T) {
	tests := []struct {
		name string
		prov bool
	}{
		{name: "without provenance"},
		{name: "with provenance", prov: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			dir := t.TempDir()
			saved := filepath.Join(dir, "chart@sha256-abc.tgz")
			target := filepath.Join(dir, "chart-1.0.0.tgz")
			r.NoError(os.WriteFile(saved, []byte("chart"), 0o600))
			if tc.prov {
				r.NoError(os.WriteFile(saved+".prov", []byte("prov"), 0o600))
			}

			got, err := renameChart(saved, target)
			r.NoError(err)
			r.Equal(target, got)

			data, err := os.ReadFile(target)
			r.NoError(err)
			r.Equal("chart", string(data))
			r.NoFileExists(saved)
			r.NoFileExists(saved + ".prov")
			if tc.prov {
				prov, err := os.ReadFile(target + ".prov")
				r.NoError(err)
				r.Equal("prov", string(prov))
			} else {
				r.NoFileExists(target + ".prov")
			}
		})
	}

	t.Run("same path is left in place", func(t *testing.T) {
		r := require.New(t)
		saved := filepath.Join(t.TempDir(), "chart-1.0.0.tgz")
		r.NoError(os.WriteFile(saved, []byte("chart"), 0o600))
		got, err := renameChart(saved, saved)
		r.NoError(err)
		r.Equal(saved, got)
		r.FileExists(saved)
	})
}

func TestNewReadOnlyChartFromRemote_ManifestDigestPin(t *testing.T) {
	tgz, err := os.ReadFile("../../testdata/mychart-0.1.0.tgz")
	require.NoError(t, err)
	srv, manifest := newChartRegistry(t, "charts/mychart", "0.1.0", tgz)
	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	chartRef := "oci://" + strings.TrimPrefix(srv.URL, "https://") + "/charts/mychart:0.1.0"

	t.Run("matching pin pulls by digest and names the file like a tag pull", func(t *testing.T) {
		r := require.New(t)
		result, err := NewReadOnlyChartFromRemote(t.Context(), chartRef, t.TempDir(),
			WithCACert(caPEM), WithOCIManifestDigest(manifest))
		r.NoError(err)
		r.Equal("mychart", result.Name)
		r.Equal("0.1.0", result.Version)

		data, err := os.ReadFile(filepath.Join(result.ChartDir, "mychart-0.1.0.tgz"))
		r.NoError(err)
		r.Equal(tgz, data)
		entries, err := os.ReadDir(result.ChartDir)
		r.NoError(err)
		r.Len(entries, 1, "the digest-named file must be renamed, not copied")
	})

	t.Run("wrong pin fails", func(t *testing.T) {
		r := require.New(t)
		_, err := NewReadOnlyChartFromRemote(t.Context(), chartRef, t.TempDir(),
			WithCACert(caPEM), WithOCIManifestDigest(godigest.FromString("other")))
		r.ErrorContains(err, `error downloading chart "`+chartRef+`" version ""`)
		r.ErrorContains(err, "not found")
	})
}

// newChartRegistry serves tgz as a helm chart at repo:tag from a read-only OCI distribution
// endpoint over TLS and returns the server and the chart's manifest digest.
func newChartRegistry(t *testing.T, repo, tag string, tgz []byte) (*httptest.Server, godigest.Digest) {
	t.Helper()
	config := []byte(`{"apiVersion":"v2","name":"mychart","version":"0.1.0"}`)
	manifest, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config: ociImageSpecV1.Descriptor{
			MediaType: registry.ConfigMediaType,
			Digest:    godigest.FromBytes(config),
			Size:      int64(len(config)),
		},
		Layers: []ociImageSpecV1.Descriptor{{
			MediaType: registry.ChartLayerMediaType,
			Digest:    godigest.FromBytes(tgz),
			Size:      int64(len(tgz)),
		}},
	})
	require.NoError(t, err)
	manifestDigest := godigest.FromBytes(manifest)

	manifests := map[string][]byte{tag: manifest, manifestDigest.String(): manifest}
	blobs := map[string][]byte{
		godigest.FromBytes(config).String(): config,
		godigest.FromBytes(tgz).String():    tgz,
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/v2/" {
			w.WriteHeader(http.StatusOK)
			return
		}
		rest, ok := strings.CutPrefix(req.URL.Path, "/v2/"+repo+"/")
		if !ok {
			http.NotFound(w, req)
			return
		}
		var data []byte
		mediaType := "application/octet-stream"
		if ref, isManifest := strings.CutPrefix(rest, "manifests/"); isManifest {
			data, mediaType = manifests[ref], ociImageSpecV1.MediaTypeImageManifest
		} else if ref, isBlob := strings.CutPrefix(rest, "blobs/"); isBlob {
			data = blobs[ref]
		}
		if data == nil {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("Docker-Content-Digest", godigest.FromBytes(data).String())
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if req.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv, manifestDigest
}
