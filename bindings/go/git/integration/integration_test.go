package integration_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/config"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/blob"
)

// trustServerCertificate changes process-global state; callers and their ancestors must not run in parallel.
// The shared HTTP factory clones the default transport, preserving TLS certificate verification.
func trustServerCertificate(t *testing.T, certificatePEM []byte) {
	t.Helper()

	r := require.New(t)

	original := http.DefaultTransport
	transport, ok := original.(*http.Transport)
	r.True(ok)
	transport = transport.Clone()
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	roots := x509.NewCertPool()
	r.True(roots.AppendCertsFromPEM(certificatePEM))
	transport.TLSClientConfig.RootCAs = roots
	http.DefaultTransport = transport
	t.Cleanup(func() {
		http.DefaultTransport = original
		transport.CloseIdleConnections()
	})
}

func newHTTPSServer(t *testing.T, path, authorization string) (url string, ca []byte) {
	t.Helper()

	r := require.New(t)

	executable, err := exec.LookPath("git")
	r.NoError(err)

	backend := &cgi.Handler{
		Path: executable,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + filepath.Dir(path), "GIT_HTTP_EXPORT_ALL=1"},
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if authorization != "" && req.Header.Get("Authorization") != authorization {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		backend.ServeHTTP(w, req)
	}))
	t.Cleanup(server.Close)

	return server.URL + "/" + filepath.Base(path), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
}

func assertArchive(t *testing.T, content blob.ReadOnlyBlob, expectedReadme string) []byte {
	t.Helper()

	r := require.New(t)

	reader, err := content.ReadCloser()
	r.NoError(err)

	data, err := io.ReadAll(reader)
	r.NoError(err)
	r.NoError(reader.Close())

	gz, err := gzip.NewReader(bytes.NewReader(data))
	r.NoError(err)
	defer func() { r.NoError(gz.Close()) }()
	tr := tar.NewReader(gz)
	header, err := tr.Next()
	r.NoError(err)
	r.Equal("README.md", header.Name)
	payload, err := io.ReadAll(tr)
	r.NoError(err)
	r.Equal(expectedReadme, string(payload))
	_, err = tr.Next()
	r.ErrorIs(err, io.EOF)

	return data
}

// newRepository creates a bare repository with two commits on main and returns
// its path and the first commit. That is all a real transport needs to exercise
// a clone for a ref and a fetch for a pinned commit; ref resolution, archive
// contents and size limits are covered against a local repository in
// internal/download.
func newRepository(t *testing.T) (path string, first plumbing.Hash) {
	t.Helper()

	r := require.New(t)

	work := t.TempDir()
	repo, err := git.PlainInit(work, false)
	r.NoError(err)
	// go-git reads the global Git config; a host commit.gpgSign must not sign fixtures.
	cfg, err := repo.Config()
	r.NoError(err)
	cfg.Commit.GpgSign = config.OptBoolFalse
	r.NoError(repo.SetConfig(cfg))
	r.NoError(repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")))

	tree, err := repo.Worktree()
	r.NoError(err)

	commit := func(content string) plumbing.Hash {
		r.NoError(os.WriteFile(filepath.Join(work, "README.md"), []byte(content), 0o600))

		_, err := tree.Add("README.md")
		r.NoError(err)

		hash, err := tree.Commit(content, &git.CommitOptions{Author: &object.Signature{
			Name:  "OCM fixture",
			Email: "fixture@example.invalid",
			When:  time.Unix(1700000000, 0).UTC(),
		}})
		r.NoError(err)

		return hash
	}

	first = commit("first\n")
	commit("second\n")

	path = filepath.Join(t.TempDir(), "fixture.git")
	_, err = git.PlainClone(path, &git.CloneOptions{URL: work, Bare: true})
	r.NoError(err)

	return path, first
}
