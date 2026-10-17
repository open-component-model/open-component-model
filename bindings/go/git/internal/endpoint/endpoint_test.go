package endpoint_test

import (
	"os"
	"path/filepath"
	"runtime"
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
		{"file:///srv/repo.git", "file", "", "", fileURL(t, "/srv/repo.git")},
		// go-git would read the host as the first element of a relative path.
		{"file://localhost/srv/repo.git", "file", "localhost", "", fileURL(t, "/srv/repo.git")},
		{"/srv/repo.git", "file", "", "", fileURL(t, "/srv/repo.git")},
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

func TestParseWindowsPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive letter paths only exist on windows")
	}
	for _, repository := range []string{`C:\srv\repo.git`, "C:/srv/repo.git", "file:///C:/srv/repo.git"} {
		t.Run(repository, func(t *testing.T) {
			r := require.New(t)

			ep, err := endpoint.Parse(repository)
			r.NoError(err)
			r.Equal("file", ep.Protocol)
			r.Equal(`C:\srv\repo.git`, ep.Path)
			r.Equal("file:///C:/srv/repo.git", ep.URL)
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
	want := filepath.Join(wd, "example.com", "org", "repo.git")
	r.Equal(want, ep.Path)
	r.Equal(fileURL(t, filepath.ToSlash(want)), ep.URL)
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

// fileURL is the URL Parse gives the slash separated path p, which on windows
// resolves against the drive of the working directory.
func fileURL(t *testing.T, p string) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.FromSlash(p))
	require.NoError(t, err)
	abs = filepath.ToSlash(abs)
	if !strings.HasPrefix(abs, "/") {
		abs = "/" + abs
	}
	return "file://" + abs
}
