package endpoint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		repository, protocol, hostname, port, url string
	}{
		{"https://example.com/org/repo.git", "https", "example.com", "443", "https://example.com/org/repo.git"},
		{"HTTPS://EXAMPLE.com/org/repo.git", "https", "example.com", "443", "HTTPS://EXAMPLE.com/org/repo.git"},
		{"http://[::1]:8080/repo.git", "http", "::1", "8080", "http://[::1]:8080/repo.git"},
		{"ssh://git@example.com:2222/org/repo.git", "ssh", "example.com", "2222", "ssh://git@example.com:2222/org/repo.git"},
		{"git@example.com:org/repo.git", "ssh", "example.com", "22", "git@example.com:org/repo.git"},
		{"git://example.com/repo.git", "git", "example.com", "9418", "git://example.com/repo.git"},
		{"file:///srv/repo.git", "file", "", "", "file:///srv/repo.git"},
		// go-git would read the host as the first element of a relative path.
		{"file://localhost/srv/repo.git", "file", "localhost", "", "file:///srv/repo.git"},
		{"/srv/repo.git", "file", "", "", "file:///srv/repo.git"},
	} {
		t.Run(tc.repository, func(t *testing.T) {
			r := require.New(t)

			ep, err := endpoint.Parse(tc.repository)
			r.NoError(err)
			r.Equal(tc.protocol, ep.Protocol)
			r.Equal(tc.hostname, ep.Host)
			r.Equal(tc.port, endpoint.Port(ep))
			r.Equal(tc.url, ep.URL)
		})
	}
}

func TestParseFragment(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, tc := range []struct {
		repository, url, ref, commit string
	}{
		{
			repository: "https://example.com/org/repo.git#branch=main",
			url:        "https://example.com/org/repo.git",
			ref:        "refs/heads/main",
		},
		{
			repository: "git@example.com:org/repo.git#tag=v1.0.0",
			url:        "git@example.com:org/repo.git",
			ref:        "refs/tags/v1.0.0",
		},
		{
			repository: "/srv/repo.git#commit=" + commit,
			url:        "file:///srv/repo.git",
			commit:     commit,
		},
		{
			repository: "https://example.com/repo.git#branch=release/1.x&commit=" + commit,
			url:        "https://example.com/repo.git",
			ref:        "refs/heads/release/1.x",
			commit:     commit,
		},
		{
			repository: "https://example.com/repo.git#",
			url:        "https://example.com/repo.git",
		},
	} {
		t.Run(tc.repository, func(t *testing.T) {
			r := require.New(t)

			ep, err := endpoint.Parse(tc.repository)
			r.NoError(err)
			r.Equal(tc.url, ep.URL)
			r.Equal(strings.Split(tc.repository, "#")[0], ep.Repository)
			r.Equal(tc.ref, ep.Ref)
			r.Equal(tc.commit, ep.Commit)
		})
	}
}

func TestSelectors(t *testing.T) {
	commit := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, repository, ref, commit, wantRef, wantCommit, err string
	}{
		{
			name:       "fields only",
			repository: "https://example.com/repo.git",
			ref:        "main",
			commit:     commit,
			wantRef:    "main",
			wantCommit: commit,
		},
		{
			name:       "fragment only",
			repository: "https://example.com/repo.git#branch=main&commit=" + commit,
			wantRef:    "refs/heads/main",
			wantCommit: commit,
		},
		{
			name:       "same short branch",
			repository: "https://example.com/repo.git#branch=main",
			ref:        "main",
			wantRef:    "refs/heads/main",
		},
		{
			name:       "same short tag",
			repository: "https://example.com/repo.git#tag=v1",
			ref:        "v1",
			wantRef:    "refs/tags/v1",
		},
		{
			name:       "same qualified branch",
			repository: "https://example.com/repo.git#branch=main",
			ref:        "refs/heads/main",
			wantRef:    "refs/heads/main",
		},
		{
			name:       "same commit in other case",
			repository: "https://example.com/repo.git#commit=" + commit,
			commit:     strings.ToUpper(commit),
			wantCommit: commit,
		},
		{
			name:       "different branch",
			repository: "https://example.com/repo.git#branch=main",
			ref:        "dev",
			err:        "conflicts",
		},
		{
			name:       "different commit",
			repository: "https://example.com/repo.git#commit=" + commit,
			commit:     strings.Repeat("b", 40),
			err:        "conflicts",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			ep, err := endpoint.Parse(tc.repository)
			r.NoError(err)
			ref, commit, err := ep.Selectors(tc.ref, tc.commit)
			if tc.err != "" {
				r.ErrorContains(err, tc.err)
				return
			}
			r.NoError(err)
			r.Equal(tc.wantRef, ref)
			r.Equal(tc.wantCommit, commit)
		})
	}
}

func TestParseRelativePath(t *testing.T) {
	r := require.New(t)
	wd, err := os.Getwd()
	r.NoError(err)

	// Without a scheme this is not a remote URL but a path relative to the working directory.
	ep, err := endpoint.Parse("example.com/org/repo.git")
	r.NoError(err)
	r.Equal("file", ep.Protocol)
	r.Equal(filepath.Join(wd, "example.com/org/repo.git"), ep.Path)
	r.Equal("file://"+filepath.Join(wd, "example.com/org/repo.git"), ep.URL)
}

func TestParseRejects(t *testing.T) {
	for _, tc := range []struct {
		repository, err string
	}{
		{"", "must not be empty"},
		{"   ", "must not be empty"},
		{"https://example.com", "requires a hostname and path"},
		{"https://example.com/", "requires a hostname and path"},
		{"https:///repo.git", "requires a hostname and path"},
		{"https://example.com:99999/repo.git", "invalid git repository port"},
		{"file://remote/srv/repo.git", "must refer to the local host"},
		{"ftp://example.com/repo.git", "unsupported git transport"},
		{"https://example.com/repo.git#ref=main", "unsupported git repository URL fragment"},
		{"https://example.com/repo.git#branch=", "requires exactly one value"},
		{"https://example.com/repo.git#branch=a&branch=b", "requires exactly one value"},
		{"https://example.com/repo.git#branch=main&tag=v1", "must not set both branch and tag"},
	} {
		t.Run(tc.repository, func(t *testing.T) {
			_, err := endpoint.Parse(tc.repository)
			require.ErrorContains(t, err, tc.err)
		})
	}
}
