package integration_test

import (
	"bytes"
	"context"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	godigest "github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/require"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content"
	"oras.land/oras-go/v2/registry/remote"
)

const s3Region = "us-east-1"

// pushArtifact pushes a single-layer OCI artifact to repository:tag in the registry at
// address and returns its manifest digest.
func pushArtifact(t *testing.T, ctx context.Context, address, repository, tag string, layer []byte) godigest.Digest {
	t.Helper()
	r := require.New(t)

	repo, err := remote.NewRepository(address + "/" + repository)
	r.NoError(err)
	repo.PlainHTTP = true

	layerDesc := content.NewDescriptorFromBytes(ocispec.MediaTypeImageLayer, layer)
	r.NoError(repo.Push(ctx, layerDesc, bytes.NewReader(layer)))
	manifest, err := oras.PackManifest(ctx, repo, oras.PackManifestVersion1_1, "application/vnd.ocm.software.test", oras.PackManifestOptions{
		Layers: []ocispec.Descriptor{layerDesc},
	})
	r.NoError(err)
	r.NoError(repo.Tag(ctx, manifest, tag))
	return manifest.Digest
}

// startFileServer starts an HTTPS server serving files by path and returns its URL.
func startFileServer(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		data, ok := files[req.URL.Path]
		if !ok {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server
}

// helmIndex is a Helm repository index listing mychart 0.1.0 at chartURL, with digest if
// it is set.
func helmIndex(chartURL string, digest godigest.Digest) []byte {
	index := fmt.Sprintf(`apiVersion: v1
entries:
  mychart:
  - apiVersion: v2
    name: mychart
    version: 0.1.0
    urls:
    - %s
`, chartURL)
	if digest != "" {
		index += fmt.Sprintf("    digest: %s\n", digest.Encoded())
	}
	return []byte(index)
}

// startGitServer serves the bare repository at path over HTTPS with git http-backend and
// returns its URL. Without a git binary it returns no server.
func startGitServer(t *testing.T, path string) (url string, server *httptest.Server) {
	t.Helper()

	executable, err := exec.LookPath("git")
	if err != nil {
		return "", nil
	}
	backend := &cgi.Handler{
		Path: executable,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + filepath.Dir(path), "GIT_HTTP_EXPORT_ALL=1"},
	}
	server = httptest.NewTLSServer(backend)
	t.Cleanup(server.Close)
	return server.URL + "/" + filepath.Base(path), server
}

// certificatePEM returns the PEM of the certificate server presents.
func certificatePEM(server *httptest.Server) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
}
