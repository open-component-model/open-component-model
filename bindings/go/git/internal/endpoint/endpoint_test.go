package endpoint_test

import (
	"os"
	"path/filepath"
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
	} {
		t.Run(tc.repository, func(t *testing.T) {
			_, err := endpoint.Parse(tc.repository)
			require.ErrorContains(t, err, tc.err)
		})
	}
}
