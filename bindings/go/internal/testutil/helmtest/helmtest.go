// Package helmtest serves Helm charts from a fake OCI registry in unit tests.
//
// [ChartRegistry] starts a read-only OCI distribution endpoint over TLS that serves one
// chart archive at repo:tag and stops it when the test ends. The returned [Registry]
// embeds the [httptest.Server], so its URL, certificate and TLS client stay available,
// and carries the chart's manifest digest:
//
//	reg := helmtest.ChartRegistry(t, "charts/mychart", "0.1.0", tgz)
//	ref := "oci://" + strings.TrimPrefix(reg.URL, "https://") + "/charts/mychart:0.1.0"
//	signed := helmtest.ChartRegistry(t, "charts/mychart", "0.1.0", tgz, helmtest.WithProvenance(prov))
package helmtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ociImageSpecV1 "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/registry"
)

// Config is the chart config blob every [ChartRegistry] serves, whatever the archive holds.
const Config = `{"apiVersion":"v2","name":"mychart","version":"0.1.0"}`

// Registry is a running fake chart registry. Manifest is the digest of the chart's manifest,
// which the registry also serves by digest.
type Registry struct {
	*httptest.Server
	Manifest digest.Digest
}

type options struct {
	prov []byte
}

// Option configures [ChartRegistry].
type Option func(*options)

// WithProvenance adds prov as the chart's provenance layer.
func WithProvenance(prov []byte) Option {
	return func(o *options) {
		o.prov = prov
	}
}

// ChartRegistry serves tgz as a Helm chart at repo:tag. It answers HEAD and GET on /v2/, on
// /v2/<repo>/manifests/<tag or digest> and on /v2/<repo>/blobs/<digest>; everything else is
// 404.
func ChartRegistry(t *testing.T, repo, tag string, tgz []byte, opts ...Option) *Registry {
	t.Helper()
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	config := []byte(Config)
	layers := []ociImageSpecV1.Descriptor{{
		MediaType: registry.ChartLayerMediaType,
		Digest:    digest.FromBytes(tgz),
		Size:      int64(len(tgz)),
	}}
	blobs := map[string][]byte{
		digest.FromBytes(config).String(): config,
		digest.FromBytes(tgz).String():    tgz,
	}
	if o.prov != nil {
		layers = append(layers, ociImageSpecV1.Descriptor{
			MediaType: registry.ProvLayerMediaType,
			Digest:    digest.FromBytes(o.prov),
			Size:      int64(len(o.prov)),
		})
		blobs[digest.FromBytes(o.prov).String()] = o.prov
	}
	manifest, err := json.Marshal(ociImageSpecV1.Manifest{
		Versioned: specs.Versioned{SchemaVersion: 2},
		MediaType: ociImageSpecV1.MediaTypeImageManifest,
		Config: ociImageSpecV1.Descriptor{
			MediaType: registry.ConfigMediaType,
			Digest:    digest.FromBytes(config),
			Size:      int64(len(config)),
		},
		Layers: layers,
	})
	require.NoError(t, err)
	manifestDigest := digest.FromBytes(manifest)
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
		w.Header().Set("Docker-Content-Digest", digest.FromBytes(data).String())
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		if req.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return &Registry{Server: srv, Manifest: manifestDigest}
}
