package endpoint_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/git/internal/endpoint"
)

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		repository, protocol, hostname, port string
	}{
		{"https://example.com/org/repo.git", "https", "example.com", "443"},
		{"HTTPS://EXAMPLE.com/org/repo.git", "https", "example.com", "443"},
		{"http://[::1]:8080/repo.git", "http", "::1", "8080"},
		{"ssh://git@example.com:2222/org/repo.git", "ssh", "example.com", "2222"},
		{"git@example.com:org/repo.git", "ssh", "example.com", "22"},
		{"git://example.com/repo.git", "git", "example.com", "9418"},
		{"file:///srv/repo.git", "file", "", ""},
		{"file://localhost/srv/repo.git", "file", "localhost", ""},
		{"/srv/repo.git", "file", "", ""},
		// Without a scheme this is not a remote URL but a relative local path.
		{"example.com/org/repo.git", "file", "", ""},
	} {
		t.Run(tc.repository, func(t *testing.T) {
			r := require.New(t)

			ep, err := endpoint.Parse(tc.repository)
			r.NoError(err)
			r.Equal(tc.protocol, ep.Protocol)
			r.Equal(tc.hostname, endpoint.Hostname(ep))
			r.Equal(tc.port, endpoint.Port(ep))
		})
	}
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
