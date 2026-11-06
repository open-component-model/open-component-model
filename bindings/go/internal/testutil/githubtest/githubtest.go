// Package githubtest fakes the GitHub archive API in tests.
//
// [Start] serves three endpoints for the repository [Owner]/[Repo]:
//
//   - /repos/<owner>/<repo>/commits/<Ref> answers with the commit the ref resolves to,
//   - /repos/<owner>/<repo>/tarball/<Commit> redirects to /codeload,
//   - /codeload serves [Server.Payload].
//
// Point a GitHub access at [Server.RepoURL] to drive the real download path against it:
//
//	srv := githubtest.Start(t)
//	access := &v1.GitHub{RepoURL: srv.RepoURL, Commit: githubtest.Commit}
//	// download, then compare the blob with srv.Payload
package githubtest

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"ocm.software/open-component-model/bindings/go/internal/testutil/tartest"
)

const (
	// Owner is the repository owner the server answers for.
	Owner = "octocat"
	// Repo is the repository name the server answers for.
	Repo = "Hello-World"
	// Ref is the only ref the server resolves.
	Ref = "main"
	// Commit is the only commit the server serves a tarball for, and the commit [Ref]
	// resolves to unless [WithCommit] says otherwise.
	Commit = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"
)

// Server is a running fake. It stops when the test ends.
type Server struct {
	// URL is the base URL of the server.
	URL string
	// RepoURL is URL followed by /<Owner>/<Repo>.
	RepoURL string
	// Payload is the exact archive the server serves.
	Payload []byte

	resolveCalls atomic.Int64

	mu            sync.Mutex
	authorization string
}

// ResolveCalls returns how many times [Ref] was resolved.
func (s *Server) ResolveCalls() int {
	return int(s.resolveCalls.Load())
}

// Authorization returns the Authorization header of the last tarball request.
func (s *Server) Authorization() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.authorization
}

type options struct {
	resolvedCommit string
	payload        []byte
}

// Option configures [Start].
type Option func(*options)

// WithCommit sets the commit [Ref] resolves to. The tarball is still served only for
// [Commit], so a different value models a ref that moved on.
func WithCommit(commit string) Option {
	return func(o *options) { o.resolvedCommit = commit }
}

// WithPayload sets the archive bytes the server serves. The default is a gzipped tar with
// one file, <Owner>-<Repo>-<Commit>/README.
func WithPayload(payload []byte) Option {
	return func(o *options) { o.payload = payload }
}

// Start runs a fake GitHub server until the test ends.
func Start(t *testing.T, opts ...Option) *Server {
	t.Helper()

	o := options{resolvedCommit: Commit}
	for _, opt := range opts {
		opt(&o)
	}
	if o.payload == nil {
		o.payload = tartest.Gzipped(t, tartest.Entry{Name: Owner + "-" + Repo + "-" + Commit + "/README", Data: []byte("hello world")})
	}

	s := &Server{Payload: o.payload}
	repoPath := "/repos/" + Owner + "/" + Repo
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, repoPath+"/commits/"+Ref):
			s.resolveCalls.Add(1)
			_, _ = w.Write([]byte(o.resolvedCommit))
		case strings.HasSuffix(r.URL.Path, repoPath+"/tarball/"+Commit):
			s.mu.Lock()
			s.authorization = r.Header.Get("Authorization")
			s.mu.Unlock()
			http.Redirect(w, r, s.URL+"/codeload", http.StatusFound)
		case r.URL.Path == "/codeload":
			_, _ = w.Write(s.Payload)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	s.URL = server.URL
	s.RepoURL = server.URL + "/" + Owner + "/" + Repo
	return s
}
