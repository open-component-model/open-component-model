package compref

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_ComponentReference(t *testing.T) {
	cases := []struct {
		input    string
		expected *Ref
		err      assert.ErrorAssertionFunc
	}{
		// Original cases
		{
			input: "github.com/open-component-model/ocm//ocm.software/ocmcli:0.23.0",
			expected: &Ref{
				Type: runtime.NewVersionedType(v1.Type, v1.Version).String(),
				Repository: &v1.OCIRepository{
					BaseUrl: "github.com/open-component-model/ocm",
				},
				Component: "ocm.software/ocmcli",
				Version:   "0.23.0",
			},
			err: assert.NoError,
		},
		{
			input: "github.com/open-component-model/ocm/component-descriptors/ocm.software/ocmcli:0.23.0",
			expected: &Ref{
				Type: runtime.NewVersionedType(v1.Type, v1.Version).String(),
				Repository: &v1.OCIRepository{
					BaseUrl: "github.com/open-component-model/ocm",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/ocmcli",
				Version:   "0.23.0",
			},
			err: assert.NoError,
		},
		{
			input: "oci::github.com/open-component-model/ocm/component-descriptors/ocm.software/ocmcli:0.23.0",
			expected: &Ref{
				Type: "oci",
				Repository: &v1.OCIRepository{
					BaseUrl: "github.com/open-component-model/ocm",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/ocmcli",
				Version:   "0.23.0",
			},
			err: assert.NoError,
		},
		{
			input: "oci::http://github.com/open-component-model/ocm/component-descriptors/ocm.software/ocmcli:0.23.0",
			expected: &Ref{
				Type: "oci",
				Repository: &v1.OCIRepository{
					BaseUrl: "http://github.com/open-component-model/ocm",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/ocmcli",
				Version:   "0.23.0",
			},
			err: assert.NoError,
		},
		{
			input: "oci::github.com:8080/open-component-model/ocm/component-descriptors/ocm.software/ocmcli:0.23.0",
			expected: &Ref{
				Type: "oci",
				Repository: &v1.OCIRepository{
					BaseUrl: "github.com:8080/open-component-model/ocm",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/ocmcli",
				Version:   "0.23.0",
			},
			err: assert.NoError,
		},
		{
			input: "./my-path//ocm.software/ocmcli:0.23.0",
			expected: &Ref{
				Type: runtime.NewVersionedType(v1.TypeCTF, v1.Version).String(),
				Repository: &v1.CTFRepository{
					Path: "./my-path",
				},
				Component: "ocm.software/ocmcli",
				Version:   "0.23.0",
			},
			err: assert.NoError,
		},
		{
			input: "ctf::./my-path//ocm.software/ocmcli:0.23.0",
			expected: &Ref{
				Type: "ctf",
				Repository: &v1.CTFRepository{
					Path: "./my-path",
				},
				Component: "ocm.software/ocmcli",
				Version:   "0.23.0",
			},
			err: assert.NoError,
		},
		{
			input: "ctf::./my-path//ocm.software/ocmcli",
			expected: &Ref{
				Type: "ctf",
				Repository: &v1.CTFRepository{
					Path: "./my-path",
				},
				Component: "ocm.software/ocmcli",
			},
			err: assert.NoError,
		},

		// Extended cases
		{
			input: "oci::https://ghcr.io/open-component-model/ocm/component-descriptors/ocm.software/cli:1.0.0",
			expected: &Ref{
				Type: "oci",
				Repository: &v1.OCIRepository{
					BaseUrl: "https://ghcr.io/open-component-model/ocm",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/cli",
				Version:   "1.0.0",
			},
			err: assert.NoError,
		},
		{
			input: "ctf::/tmp/ctfrepo/component-descriptors/ocm.software/cli:0.1.0",
			expected: &Ref{
				Type: "ctf",
				Repository: &v1.CTFRepository{
					Path: "/tmp/ctfrepo",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/cli",
				Version:   "0.1.0",
			},
			err: assert.NoError,
		},
		{
			input: "./relative/path/component-descriptors/ocm.software/component:2.0.0",
			expected: &Ref{
				Type: runtime.NewVersionedType(v1.TypeCTF, v1.Version).String(),
				Repository: &v1.CTFRepository{
					Path: "./relative/path",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/component",
				Version:   "2.0.0",
			},
			err: assert.NoError,
		},
		{
			input: "github.com/mandelsoft/ocm/component-descriptors/ocm.software/component",
			expected: &Ref{
				Type: runtime.NewVersionedType(v1.Type, v1.Version).String(),
				Repository: &v1.OCIRepository{
					BaseUrl: "github.com/mandelsoft/ocm",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/component",
			},
			err: assert.NoError,
		},
		{
			input: "oci::ghcr.io/project/component-descriptors/ocm.software/cmp:0.5.1",
			expected: &Ref{
				Type: "oci",
				Repository: &v1.OCIRepository{
					BaseUrl: "ghcr.io/project",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/cmp",
				Version:   "0.5.1",
			},
			err: assert.NoError,
		},
		{
			input: "/absolute/path/component-descriptors/ocm.software/cmp:0.5.1",
			expected: &Ref{
				Type: runtime.NewVersionedType(v1.TypeCTF, v1.Version).String(),
				Repository: &v1.CTFRepository{
					Path: "/absolute/path",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/cmp",
				Version:   "0.5.1",
			},
			err: assert.NoError,
		},
		{
			input: "oci::localhost:5000/open-component-model/ocm/component-descriptors/ocm.software/test:1.2.3",
			expected: &Ref{
				Type: "oci",
				Repository: &v1.OCIRepository{
					BaseUrl: "localhost:5000/open-component-model/ocm",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/test",
				Version:   "1.2.3",
			},
			err: assert.NoError,
		},
		{
			input: "localhost:5000/open-component-model/ocm/component-descriptors/ocm.software/test:1.2.3",
			expected: &Ref{
				Type: runtime.NewVersionedType(v1.Type, v1.Version).String(),
				Repository: &v1.OCIRepository{
					BaseUrl: "localhost:5000/open-component-model/ocm",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/test",
				Version:   "1.2.3",
			},
			err: assert.NoError,
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("case-%02d", i+1), func(t *testing.T) {
			t.Logf("%q", tc.input)
			r := require.New(t)
			parsed, err := Parse(tc.input)

			if tc.err(t, err) && err == nil {
				r.Equalf(tc.expected, parsed, "input %q was incorrectly parsed", tc.input)
			}
			if parsed != nil && tc.expected != nil {
				r.Contains(parsed.String(), tc.expected.Component, "input %q did not serialize properly", tc.input)
			}
		})
	}
}
