package v1_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/credentials"
	config "ocm.software/open-component-model/bindings/go/credentials/spec/config/runtime"
	directv1 "ocm.software/open-component-model/bindings/go/credentials/spec/config/v1"
	v1 "ocm.software/open-component-model/bindings/go/npm/spec/identity/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestIdentityFromRegistryAndPackage(t *testing.T) {
	for _, tc := range []struct {
		registry, pkg            string
		scheme, host, port, path string
	}{
		{"https://registry.npmjs.org", "lodash", "https", "registry.npmjs.org", "", "lodash"},
		// a trailing slash on the registry must not double up in the path
		{"https://registry.npmjs.org/", "lodash", "https", "registry.npmjs.org", "", "lodash"},
		{"https://registry.npmjs.org", "@types/node", "https", "registry.npmjs.org", "", "@types/node"},
		{"http://localhost:4873", "lodash", "http", "localhost", "4873", "lodash"},
		{"https://npm.example.com/artifactory/api/npm/npm-repo", "@scope/pkg", "https", "npm.example.com", "", "artifactory/api/npm/npm-repo/@scope/pkg"},
		// the package is optional, so an identity can address a whole registry
		{"https://registry.npmjs.org", "", "https", "registry.npmjs.org", "", ""},
	} {
		t.Run(tc.registry+"/"+tc.pkg, func(t *testing.T) {
			r := require.New(t)

			id, err := v1.IdentityFromRegistryAndPackage(tc.registry, tc.pkg)
			r.NoError(err)
			r.Equal("NpmRegistry", id["type"])
			r.Equal(tc.scheme, id["scheme"])
			r.Equal(tc.host, id["hostname"])
			r.Equal(tc.port, id["port"])
			r.Equal(tc.path, id["path"])
		})
	}

	r := require.New(t)

	_, err := v1.IdentityFromRegistryAndPackage("", "lodash")
	r.ErrorContains(err, "registry is required")
}

func TestIdentityCredentialGraph(t *testing.T) {
	for _, tc := range []struct {
		name     string
		identity runtime.Identity
		pkg      string
		matches  bool
	}{
		{"whole registry", runtime.Identity{"type": "NpmRegistry", "hostname": "registry.npmjs.org", "scheme": "https"}, "lodash", true},
		{"scope", runtime.Identity{"type": "NpmRegistry", "hostname": "registry.npmjs.org", "scheme": "https", "path": "@types/*"}, "@types/node", true},
		{"other scope", runtime.Identity{"type": "NpmRegistry", "hostname": "registry.npmjs.org", "scheme": "https", "path": "@types/*"}, "@babel/core", false},
		{"single package", runtime.Identity{"type": "NpmRegistry", "hostname": "registry.npmjs.org", "scheme": "https", "path": "lodash"}, "lodash", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			direct := &directv1.DirectCredentials{
				Type:       runtime.NewVersionedType(directv1.CredentialsType, directv1.Version),
				Properties: map[string]string{"token": "fixture"},
			}

			graph, err := credentials.ToGraph(t.Context(), &config.Config{Consumers: []config.Consumer{{Identities: []runtime.Identity{tc.identity}, Credentials: []runtime.Typed{direct}}}}, credentials.Options{})
			r.NoError(err)

			id, err := v1.IdentityFromRegistryAndPackage("https://registry.npmjs.org", tc.pkg)
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
