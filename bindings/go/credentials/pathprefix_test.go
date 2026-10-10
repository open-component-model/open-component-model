package credentials_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	credentialruntime "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	v1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestIngestLegacyPathPrefix checks that a config-authored pathprefix matches the same
// request paths as the OCM v1 hostpath matcher (ocm.software/ocm/api/credentials/identity/hostpath).
func TestIngestLegacyPathPrefix(t *testing.T) {
	for _, tc := range []struct {
		name       string
		pathPrefix string
		// requestPath is omitted from the request identity when empty.
		requestPath string
		match       bool
	}{
		{"prefix itself", "a/b", "a/b", true},
		{"one level below", "a/b", "a/b/c", true},
		{"two levels below", "a/b", "a/b/c/d", true},
		{"suffix in the same segment", "a/b", "a/b.git", false},
		{"sibling with the same prefix", "a/b", "a/bc", false},
		{"parent", "a/b", "a", false},
		{"leading slash is trimmed", "/a/b", "a/b/c", true},
		{"trailing slash matches nothing below", "a/b/", "a/b/c", false},
		{"empty prefix matches every path", "", "a/b", true},
		{"request without path", "a", "", false},
		{"star is literal", "a/*", "a/b", false},
		{"star matches itself", "a/*", "a/*", true},
		{"braces are literal", "a/{b,c}", "a/b", false},
		{"braces match themselves", "a/{b,c}", "a/{b,c}", true},
		{"caret class is literal", "a/[^b]", "a/c", false},
		{"caret class matches itself", "a/[^b]", "a/[^b]", true},
		{"bang class is literal", "a/[!b]", "a/c", false},
		{"question mark is literal", "a/b?", "a/bc", false},
		{"backslash is literal", `a/b\c`, `a/b\c`, true},
		{"comma is literal", "a/b,c", "a/b,c/d", true},
		{"comma does not split the prefix", "a/b,c", "a/b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			graph, err := credentials.ToGraph(t.Context(), pathPrefixConfig(runtime.Identity{
				runtime.IdentityAttributeType:     "Git",
				runtime.IdentityAttributeHostname: "git.example.com",
				"pathprefix":                      tc.pathPrefix,
			}), credentials.Options{})
			r.NoError(err)

			request := runtime.Identity{
				runtime.IdentityAttributeType:     "Git",
				runtime.IdentityAttributeHostname: "git.example.com",
			}
			if tc.requestPath != "" {
				request[runtime.IdentityAttributePath] = tc.requestPath
			}

			resolved, err := graph.Resolve(t.Context(), request)
			if !tc.match {
				r.ErrorIs(err, credentials.ErrNotFound)
				return
			}
			r.NoError(err)
			r.Equal("alice", resolved.(*v1.DirectCredentials).Properties["username"])
		})
	}

	t.Run("path wins over pathprefix", func(t *testing.T) {
		r := require.New(t)

		graph, err := credentials.ToGraph(t.Context(), pathPrefixConfig(runtime.Identity{
			runtime.IdentityAttributeType:     "Git",
			runtime.IdentityAttributeHostname: "git.example.com",
			runtime.IdentityAttributePath:     "a/*",
			"pathprefix":                      "b",
		}), credentials.Options{})
		r.NoError(err)

		_, err = graph.Resolve(t.Context(), runtime.Identity{
			runtime.IdentityAttributeType:     "Git",
			runtime.IdentityAttributeHostname: "git.example.com",
			runtime.IdentityAttributePath:     "a/x",
		})
		r.NoError(err)

		_, err = graph.Resolve(t.Context(), runtime.Identity{
			runtime.IdentityAttributeType:     "Git",
			runtime.IdentityAttributeHostname: "git.example.com",
			runtime.IdentityAttributePath:     "b/x",
		})
		r.ErrorIs(err, credentials.ErrNotFound)
	})
}

func pathPrefixConfig(identity runtime.Identity) *credentialruntime.Config {
	return &credentialruntime.Config{
		Consumers: []credentialruntime.Consumer{{
			Identities: []runtime.Identity{identity},
			Credentials: []runtime.Typed{&v1.DirectCredentials{
				Type:       runtime.NewVersionedType(v1.CredentialsType, v1.Version),
				Properties: map[string]string{"username": "alice"},
			}},
		}},
	}
}
