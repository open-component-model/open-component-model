package git

import (
	"crypto/fips140"
	"net/http"
	"net/http/httptest"
	"path/filepath"

	"github.com/go-git/go-billy/v6/osfs"
	"github.com/go-git/go-git/v6/backend"
	"github.com/go-git/go-git/v6/plumbing/transport"
)

// TestingT is the part of testing.TB that Serve uses. A testing.TB and ginkgo's
// GinkgoT() satisfy it.
type TestingT interface {
	Helper()
	Cleanup(func())
}

// Serve serves the repository at dir over plain HTTP from an in-process go-git
// backend and returns its URL. dir can be a bare repository or a work tree. The
// server accepts pushes without credentials and stops when the test ends.
//
// Git/v1 accesses reject local repositories, so a test that uses a local fixture
// must serve it under a remote URL.
func Serve(t TestingT, dir string) string {
	t.Helper()
	b := backend.New(transport.NewFilesystemLoader(osfs.New(filepath.Dir(dir)), false))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The backend refuses receive-pack without an Authorization header.
		r.Header.Set("Authorization", "Basic dGVzdDp0ZXN0")
		// The server is test infrastructure that hashes objects with SHA-1, so it
		// runs without FIPS 140 enforcement. The client under test stays enforced.
		fips140.WithoutEnforcement(func() { b.ServeHTTP(w, r) })
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/" + filepath.Base(dir)
}
