package v1_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/npm/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func access(registry, pkg, version string) *v1.NPM {
	return &v1.NPM{
		Type:     runtime.NewVersionedType(v1.Type, v1.Version),
		Registry: registry,
		Package:  pkg,
		Version:  version,
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		registry, pkg, version string
		wantErr                string
	}{
		{name: "plain package", registry: "https://registry.npmjs.org", pkg: "lodash", version: "4.17.21"},
		{name: "scoped package", registry: "https://registry.npmjs.org", pkg: "@types/node", version: "20.11.5"},
		{name: "dotted package", registry: "https://registry.npmjs.org", pkg: "@scope/some.pkg_name-1", version: "1.0.0"},
		{name: "registry with path", registry: "https://npm.example.com/artifactory/api/npm/npm-repo", pkg: "lodash", version: "4.17.21"},
		{name: "http registry", registry: "http://localhost:4873", pkg: "lodash", version: "4.17.21"},
		{name: "prerelease", registry: "https://registry.npmjs.org", pkg: "lodash", version: "1.0.0-rc.1+build.5"},
		// names that predate npm's current rules are still served by the registry,
		// and an access type has to be able to point at them
		{name: "legacy upper case package", registry: "https://registry.npmjs.org", pkg: "JSONStream", version: "1.3.5"},
		{name: "legacy single letter package", registry: "https://registry.npmjs.org", pkg: "Q", version: "1.5.1"},
		{name: "legacy special characters", registry: "https://registry.npmjs.org", pkg: "the~package!", version: "1.0.0"},
		{name: "legacy scoped mixed case", registry: "https://registry.npmjs.org", pkg: "@Scope/Name", version: "1.0.0"},

		{name: "no registry", pkg: "lodash", version: "4.17.21", wantErr: "registry is required"},
		{name: "no package", registry: "https://registry.npmjs.org", version: "4.17.21", wantErr: "package is required"},
		{name: "no version", registry: "https://registry.npmjs.org", pkg: "lodash", wantErr: "version is required"},
		{name: "ssh registry", registry: "ssh://registry.npmjs.org", pkg: "lodash", version: "4.17.21", wantErr: "must use the http or https scheme"},
		{name: "schemeless registry", registry: "registry.npmjs.org", pkg: "lodash", version: "4.17.21", wantErr: "must use the http or https scheme"},
		{name: "registry without host", registry: "https://", pkg: "lodash", version: "4.17.21", wantErr: "has no host"},
		{name: "leading dot package", registry: "https://registry.npmjs.org", pkg: ".lodash", version: "4.17.21", wantErr: "must not start with a period or an underscore"},
		{name: "leading underscore package", registry: "https://registry.npmjs.org", pkg: "_lodash", version: "4.17.21", wantErr: "must not start with a period or an underscore"},
		{name: "url unsafe package", registry: "https://registry.npmjs.org", pkg: "lo%64ash", version: "4.17.21", wantErr: "invalid package name"},
		{name: "scope without name", registry: "https://registry.npmjs.org", pkg: "@scope/", version: "4.17.21", wantErr: "invalid package name"},
		{name: "unscoped slash package", registry: "https://registry.npmjs.org", pkg: "scope/name", version: "4.17.21", wantErr: "invalid package name"},
		{name: "space in package", registry: "https://registry.npmjs.org", pkg: "lo dash", version: "4.17.21", wantErr: "invalid package name"},
		{name: "caret range", registry: "https://registry.npmjs.org", pkg: "lodash", version: "^4.17.21", wantErr: "exact semantic version"},
		{name: "tilde range", registry: "https://registry.npmjs.org", pkg: "lodash", version: "~4.17.21", wantErr: "exact semantic version"},
		{name: "comparison range", registry: "https://registry.npmjs.org", pkg: "lodash", version: ">=4.0.0 <5.0.0", wantErr: "exact semantic version"},
		{name: "wildcard range", registry: "https://registry.npmjs.org", pkg: "lodash", version: "4.17.x", wantErr: "exact semantic version"},
		{name: "partial version", registry: "https://registry.npmjs.org", pkg: "lodash", version: "4.17", wantErr: "exact semantic version"},
		{name: "dist-tag", registry: "https://registry.npmjs.org", pkg: "lodash", version: "latest", wantErr: "exact semantic version"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)

			err := access(tc.registry, tc.pkg, tc.version).Validate()
			if tc.wantErr == "" {
				r.NoError(err)
				return
			}
			r.ErrorContains(err, tc.wantErr)
		})
	}

	t.Run("long package name", func(t *testing.T) {
		r := require.New(t)

		long := make([]byte, 215)
		for i := range long {
			long[i] = 'a'
		}
		r.ErrorContains(access("https://registry.npmjs.org", string(long), "1.0.0").Validate(), "214 characters")
	})

	t.Run("nil access", func(t *testing.T) {
		r := require.New(t)

		var spec *v1.NPM
		r.ErrorContains(spec.Validate(), "npm access is required")
	})
}
