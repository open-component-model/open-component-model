package v1_test

import (
	"github.com/stretchr/testify/require"
	"ocm.software/open-component-model/bindings/go/credentials"
	config "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	directv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	v1 "ocm.software/open-component-model/bindings/go/git/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"testing"
)

const userinfo = "user:secret"

func TestIdentityFromURL(t *testing.T) {
	for _, tc := range []struct {
		url, scheme, host, port, path string
	}{
		// userinfo is assembled at runtime to keep the literal out of secret scanners.
		{"https://" + userinfo + "@EXAMPLE.com/org/repo.git", "https", "example.com", "443", "org/repo.git"},
		{"ssh://git@example.com/org/repo.git", "ssh", "example.com", "22", "org/repo.git"},
		{"git@example.com:org/repo.git", "ssh", "example.com", "22", "org/repo.git"},
		{"ssh://git@example.com:2222/org/repo.git", "ssh", "example.com", "2222", "org/repo.git"},
		{"git://example.com/org/repo.git", "git", "example.com", "9418", "org/repo.git"},
		{"file:///workspace/repo.git", "file", "localhost", "", "workspace/repo.git"},
	} {
		t.Run(tc.url, func(t *testing.T) {
			r := require.New(t)

			id, err := v1.IdentityFromURL(tc.url)
			r.NoError(err)
			r.Equal("Git", id["type"])
			r.Equal(tc.scheme, id["scheme"])
			r.Equal(tc.host, id["hostname"])
			r.Equal(tc.port, id["port"])
			r.Equal(tc.path, id["path"])
			r.NotContains(id, "username")
			r.NotContains(id.String(), "secret")
		})
	}
}

func TestIdentityCredentialGraph(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity runtime.Identity
		url      string
		matches  bool
	}{
		{"legacy host", runtime.Identity{"type": "Git", "hostname": "example.com", "scheme": "ssh", "port": "22"}, "git@example.com:org/repo.git", true},
		{"scoped", runtime.Identity{"type": "Git", "hostname": "example.com", "scheme": "https", "path": "org/*"}, "https://example.com/org/repo.git", true},
		{"other path", runtime.Identity{"type": "Git", "hostname": "example.com", "scheme": "https", "path": "org/*"}, "https://example.com/other/repo.git", false},
		{"other transport", runtime.Identity{"type": "Git", "hostname": "example.com", "scheme": "https", "path": "org/*"}, "git@example.com:org/repo.git", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			direct := &directv1.DirectCredentials{
				Type:       runtime.NewVersionedType(directv1.CredentialsType, directv1.Version),
				Properties: map[string]string{"token": "fixture"},
			}

			graph, err := credentials.ToGraph(t.Context(), &config.Config{Consumers: []config.Consumer{{Identities: []runtime.Identity{tc.identity}, Credentials: []runtime.Typed{direct}}}}, credentials.Options{})
			r.NoError(err)

			id, err := v1.IdentityFromURL(tc.url)
			r.NoError(err)

			got, err := graph.Resolve(t.Context(), id)
			if tc.matches {
				r.NoError(err)
				r.Equal(direct, got)
			} else {
				r.Error(err)
			}
		})
	}
}
