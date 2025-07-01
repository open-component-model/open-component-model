package compref

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func Test_ComponentReference(t *testing.T) {
	cases := []struct {
		input    string
		opts     ParseOptions
		expected *Ref
		err      assert.ErrorAssertionFunc
	}{
		{
			input: "github.com/open-component-model/ocm//ocm.software/ocmcli:0.23.0",
			expected: &Ref{
				Type: runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(),
				Repository: &ociv1.Repository{
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
				Type: runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(),
				Repository: &ociv1.Repository{
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
				Repository: &ociv1.Repository{
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
				Repository: &ociv1.Repository{
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
				Repository: &ociv1.Repository{
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
				Type: runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(),
				Repository: &ctfv1.Repository{
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
				Repository: &ctfv1.Repository{
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
				Repository: &ctfv1.Repository{
					Path: "./my-path",
				},
				Component: "ocm.software/ocmcli",
			},
			err: assert.NoError,
		},

		{
			input: "oci::https://ghcr.io/open-component-model/ocm/component-descriptors/ocm.software/cli:1.0.0",
			expected: &Ref{
				Type: "oci",
				Repository: &ociv1.Repository{
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
				Repository: &ctfv1.Repository{
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
				Type: runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(),
				Repository: &ctfv1.Repository{
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
				Type: runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(),
				Repository: &ociv1.Repository{
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
				Repository: &ociv1.Repository{
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
				Type: runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String(),
				Repository: &ctfv1.Repository{
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
				Repository: &ociv1.Repository{
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
				Type: runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(),
				Repository: &ociv1.Repository{
					BaseUrl: "localhost:5000/open-component-model/ocm",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/test",
				Version:   "1.2.3",
			},
			err: assert.NoError,
		},
		{
			input: "github.com/open-component-model/ocm//ocm.software/ocmcli:0.23.0@sha256:5b0bcabd1ed22e9fb1310cf6c2dec7cdef19f0ad69efa1f392e94a4333501270",
			expected: &Ref{
				Type: runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(),
				Repository: &ociv1.Repository{
					BaseUrl: "github.com/open-component-model/ocm",
				},
				Component: "ocm.software/ocmcli",
				Version:   "0.23.0",
				Digest:    "sha256:5b0bcabd1ed22e9fb1310cf6c2dec7cdef19f0ad69efa1f392e94a4333501270",
			},
			err: assert.NoError,
		},
		{
			input: "github.com/open-component-model/ocm//ocm.software/ocmcli@sha256:5b0bcabd1ed22e9fb1310cf6c2dec7cdef19f0ad69efa1f392e94a4333501270",
			expected: &Ref{
				Type: runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(),
				Repository: &ociv1.Repository{
					BaseUrl: "github.com/open-component-model/ocm",
				},
				Component: "ocm.software/ocmcli",
				Digest:    "sha256:5b0bcabd1ed22e9fb1310cf6c2dec7cdef19f0ad69efa1f392e94a4333501270",
			},
			err: assert.NoError,
		},
		{
			input: "myrepo//ocm.software/ocmcli:0.23.0",
			opts: ParseOptions{
				Aliases: map[string]*runtime.Raw{
					"myrepo": {
						Type: runtime.NewVersionedType(ociv1.Type, ociv1.Version),
						Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"github.com/open-component-model/ocm"}`),
					},
				},
			},
			expected: &Ref{
				Type: runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(),
				Repository: &ociv1.Repository{
					BaseUrl: "github.com/open-component-model/ocm",
				},
				Component: "ocm.software/ocmcli",
				Version:   "0.23.0",
			},
			err: assert.NoError,
		},
		{
			input: "myrepo//component-descriptors/ocm.software/ocmcli:0.23.0",
			opts: ParseOptions{
				Aliases: map[string]*runtime.Raw{
					"myrepo": {
						Type: runtime.NewVersionedType(ociv1.Type, ociv1.Version),
						Data: []byte(`{"type":"OCIRepository/v1","baseUrl":"github.com/open-component-model/ocm"}`),
					},
				},
			},
			expected: &Ref{
				Type: runtime.NewVersionedType(ociv1.Type, ociv1.Version).String(),
				Repository: &ociv1.Repository{
					BaseUrl: "github.com/open-component-model/ocm",
				},
				Prefix:    "component-descriptors",
				Component: "ocm.software/ocmcli",
				Version:   "0.23.0",
			},
			err: assert.NoError,
		},
	}

	for i, tc := range cases {
		t.Run(fmt.Sprintf("case-%02d", i+1), func(t *testing.T) {
			t.Logf("%q", tc.input)
			r := require.New(t)
			parsed, err := Parse(tc.input, &tc.opts)
			if tc.expected.Type != "" {
				if typ, err := runtime.TypeFromString(parsed.Type); err == nil {
					tc.expected.Repository.SetType(typ)
				}
			}
			if tc.err(t, err) && err == nil {
				r.Equalf(tc.expected, parsed, "input %q was incorrectly parsed", tc.input)
			}
			if parsed != nil && tc.expected != nil {
				r.Contains(parsed.String(), tc.expected.Component, "input %q did not serialize properly", tc.input)
			}
		})
	}
}

func Test_ComponentReference_Permutations(t *testing.T) {
	typePart := []struct {
		prefix string
	}{
		{""},
		{"oci::"},
		{runtime.NewVersionedType(ociv1.Type, ociv1.Version).String() + "::"},
		{"ctf::"},
		{runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String() + "::"},
	}

	repoPart := []struct {
		input string
		oci   bool
	}{
		{"https://github.com/open-component-model/ocm", true},
		{"http://github.com/open-component-model/ocm", true},
		{"oci://github.com/open-component-model/ocm", true},
		{"github.com/open-component-model/ocm", true},
		{"localhost:5000/open-component-model/ocm", true},
		{"./local/path", false},
		{"file://./local/path", false},
		{"/absolute/path", false},
	}

	prefixes := []string{
		"", // No prefix
		DefaultPrefix,
	}

	components := []string{
		"ocm.software/cli",
		"ocm.software/ocmcli",
	}

	versions := []string{
		"", ":1.2.3", ":v0.1.0",
	}

	digests := []string{
		"", "@sha256:5b0bcabd1ed22e9fb1310cf6c2dec7cdef19f0ad69efa1f392e94a4333501270",
	}

	i := 0
	for _, repoTypePrefix := range typePart {
		for _, repo := range repoPart {
			for _, repositoryPrefix := range prefixes {
				for _, componentName := range components {
					for _, componentVersion := range versions {
						for _, componentDigest := range digests {
							// build reference string
							repositoryInput := repoTypePrefix.prefix + repo.input
							repositoryInput += "/" + repositoryPrefix + "/"
							repositoryInput += componentName + componentVersion + componentDigest

							var typ string
							switch repoTypePrefix.prefix {
							case "":
								if repo.oci {
									typ = runtime.NewVersionedType(ociv1.Type, ociv1.Version).String()
								} else {
									typ = runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String()
								}
							default:
								typ = strings.TrimSuffix(repoTypePrefix.prefix, "::")
							}

							// build expected Ref
							var expectedRepository runtime.Typed
							switch typ {
							case "oci", runtime.NewVersionedType(ociv1.Type, ociv1.Version).String():
								expectedRepository = &ociv1.Repository{BaseUrl: repo.input}
							case "ctf", runtime.NewVersionedType(ctfv1.Type, ctfv1.Version).String():
								expectedRepository = &ctfv1.Repository{Path: repo.input}
							}

							expected := &Ref{
								Type:       typ,
								Repository: expectedRepository,
								Prefix:     repositoryPrefix,
								Component:  componentName,
							}
							if expected.Type != "" {
								if typ, err := runtime.TypeFromString(expected.Type); err == nil {
									expected.Repository.SetType(typ)
								}
							}

							if strings.HasPrefix(componentVersion, ":") {
								expected.Version = componentVersion[1:]
							}
							if strings.HasPrefix(componentDigest, "@") {
								expected.Digest = componentDigest[1:]
							}

							t.Run(fmt.Sprintf("perm-%03d", i), func(t *testing.T) {
								t.Logf("%q", repositoryInput)
								parsed, err := Parse(repositoryInput, &ParseOptions{})
								if !assert.NoError(t, err) {
									return
								}
								a := assert.New(t)
								a.Equalf(expected, parsed, "input %q was incorrectly parsed", repositoryInput)
								a.Containsf(parsed.String(), componentName, "input %q did not serialize properly", repositoryInput)
							})
							i++
						}
					}
				}
			}
		}
	}
}
