// Package gittest provides shared test fixtures for git HTTP server helpers.
// It is internal to the bindings/go module; test packages outside the module
// cannot import it.
package gittest

import (
	"net/http/cgi" //nolint:gosec // G504: git-http-backend is a child process; the httpoxy CVE-2016-5386 attack does not apply here
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/stretchr/testify/require"
)

// ServeHTTP starts a git-http-backend HTTP server (plain HTTP, no TLS) that
// serves bareDir with both fetch (upload-pack) and push (receive-pack) enabled.
// The server is stopped automatically when t finishes.
//
// The git binary runs as a child process and is not subject to Go's FIPS mode,
// so this helper is safe to call from FIPS-only test binaries.
//
// bareDir must be a bare git repository. The returned URL has the form
// http://127.0.0.1:<PORT>/<basename of bareDir>.
func ServeHTTP(t *testing.T, bareDir string) string {
	t.Helper()
	r := require.New(t)

	gitBin, err := exec.LookPath("git")
	r.NoError(err, "git binary must be in PATH to run git HTTP server tests")

	out, err := exec.CommandContext(t.Context(), gitBin, "-C", bareDir, "config", "http.receivepack", "true").CombinedOutput()
	r.NoError(err, "failed to enable http.receivepack: %s", string(out))

	backend := &cgi.Handler{
		Path: gitBin,
		Args: []string{"http-backend"},
		Env: []string{
			"GIT_PROJECT_ROOT=" + filepath.Dir(bareDir),
			"GIT_HTTP_EXPORT_ALL=1",
			"GIT_HTTP_RECEIVE_PACK=1",
		},
	}
	srv := httptest.NewServer(backend)
	t.Cleanup(srv.Close)
	return srv.URL + "/" + filepath.Base(bareDir)
}

// NewEmptyServer creates a new empty bare git repository under a temp directory
// and serves it with ServeHTTP. Tests can push commits into the returned URL.
func NewEmptyServer(t *testing.T) string {
	t.Helper()
	r := require.New(t)

	bareParent := t.TempDir()
	barePath := filepath.Join(bareParent, "fixture.git")
	_, err := git.PlainInit(barePath, true)
	r.NoError(err)

	return ServeHTTP(t, barePath)
}

// NewServerFrom bare-clones sourcePath into a temp directory and serves the
// result with ServeHTTP. sourcePath may be a local directory or a remote URL.
func NewServerFrom(t *testing.T, sourcePath string) string {
	t.Helper()
	r := require.New(t)

	bareParent := t.TempDir()
	barePath := filepath.Join(bareParent, "fixture.git")
	_, err := git.PlainClone(barePath, &git.CloneOptions{URL: sourcePath, Bare: true})
	r.NoError(err)

	return ServeHTTP(t, barePath)
}
