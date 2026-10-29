package download

import (
	"bytes"
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

	helmcredsv1 "ocm.software/open-component-model/bindings/go/helm/spec/credentials/v1"
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

func TestPinnedChartName(t *testing.T) {
	tests := []struct {
		name, ref, version, want string
	}{
		{name: "tag", ref: "oci://registry.example/charts/mychart:1.0.0", want: "mychart-1.0.0.tgz"},
		{name: "version overrides missing tag", ref: "oci://registry.example/charts/mychart", version: "1.0.0", want: "mychart-1.0.0.tgz"},
		{name: "repository base, not chart name", ref: "oci://registry.example/charts/renamed:1.0.0", want: "renamed-1.0.0.tgz"},
		{name: "no tag falls back to chart metadata", ref: "oci://registry.example/charts/mychart"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			got, err := pinnedChartName(tc.ref, tc.version)
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
	srv, manifest := newChartRegistry(t, "charts/mychart", "0.1.0", tgz, nil)
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

	t.Run("pinned file is named after the repository and tag, not the chart", func(t *testing.T) {
		r := require.New(t)
		renamed, renamedManifest := newChartRegistry(t, "charts/renamed", "0.1.0", tgz, nil)
		renamedCA := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: renamed.Certificate().Raw}))
		ref := "oci://" + strings.TrimPrefix(renamed.URL, "https://") + "/charts/renamed:0.1.0"
		result, err := NewReadOnlyChartFromRemote(t.Context(), ref, t.TempDir(),
			WithCACert(renamedCA), WithOCIManifestDigest(renamedManifest))
		r.NoError(err)
		r.Equal("mychart", result.Name)
		_, err = os.Stat(filepath.Join(result.ChartDir, "renamed-0.1.0.tgz"))
		r.NoError(err)
	})
}

func TestNewReadOnlyChartFromRemote_ManifestDigestPinWithKeyring(t *testing.T) {
	tgz, err := os.ReadFile("../../testdata/provenance/mychart-0.1.0.tgz")
	require.NoError(t, err)
	prov, err := os.ReadFile("../../testdata/provenance/mychart-0.1.0.tgz.prov")
	require.NoError(t, err)
	keyring, err := filepath.Abs("../../testdata/provenance/pub.gpg")
	require.NoError(t, err)
	srv, manifest := newChartRegistry(t, "charts/mychart", "0.1.0", tgz, prov)
	caPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	chartRef := "oci://" + strings.TrimPrefix(srv.URL, "https://") + "/charts/mychart:0.1.0"

	t.Run("provenance verifies against the renamed file", func(t *testing.T) {
		r := require.New(t)
		result, err := NewReadOnlyChartFromRemote(t.Context(), chartRef, t.TempDir(), WithCACert(caPEM),
			WithCredentials(&helmcredsv1.HelmHTTPCredentials{Keyring: keyring}), WithOCIManifestDigest(manifest))
		r.NoError(err)
		r.NotNil(result.ProvBlob)
		_, err = os.Stat(filepath.Join(result.ChartDir, "mychart-0.1.0.tgz.prov"))
		r.NoError(err)
	})

	t.Run("provenance of another chart fails", func(t *testing.T) {
		r := require.New(t)
		tampered := bytes.Replace(prov, []byte("mychart-0.1.0.tgz"), []byte("mychart-0.2.0.tgz"), 1)
		other, otherManifest := newChartRegistry(t, "charts/mychart", "0.1.0", tgz, tampered)
		otherCA := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: other.Certificate().Raw}))
		ref := "oci://" + strings.TrimPrefix(other.URL, "https://") + "/charts/mychart:0.1.0"
		_, err := NewReadOnlyChartFromRemote(t.Context(), ref, t.TempDir(), WithCACert(otherCA),
			WithCredentials(&helmcredsv1.HelmHTTPCredentials{Keyring: keyring}), WithOCIManifestDigest(otherManifest))
		r.ErrorContains(err, `error verifying provenance of chart "`+ref+`"`)
	})
}

// newChartRegistry serves tgz (and, if non-nil, its provenance prov) as a helm chart at repo:tag
// from a read-only OCI distribution endpoint over TLS and returns the server and the chart's
// manifest digest.
func newChartRegistry(t *testing.T, repo, tag string, tgz, prov []byte) (*httptest.Server, godigest.Digest) {
	t.Helper()
	config := []byte(`{"apiVersion":"v2","name":"mychart","version":"0.1.0"}`)
	layers := []ociImageSpecV1.Descriptor{{
		MediaType: registry.ChartLayerMediaType,
		Digest:    godigest.FromBytes(tgz),
		Size:      int64(len(tgz)),
	}}
	blobs := map[string][]byte{
		godigest.FromBytes(config).String(): config,
		godigest.FromBytes(tgz).String():    tgz,
	}
	if prov != nil {
		layers = append(layers, ociImageSpecV1.Descriptor{
			MediaType: registry.ProvLayerMediaType,
			Digest:    godigest.FromBytes(prov),
			Size:      int64(len(prov)),
		})
		blobs[godigest.FromBytes(prov).String()] = prov
	}
	manifest, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config: ociImageSpecV1.Descriptor{
			MediaType: registry.ConfigMediaType,
			Digest:    godigest.FromBytes(config),
			Size:      int64(len(config)),
		},
		Layers: layers,
	})
	require.NoError(t, err)
	manifestDigest := godigest.FromBytes(manifest)

	manifests := map[string][]byte{tag: manifest, manifestDigest.String(): manifest}
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
