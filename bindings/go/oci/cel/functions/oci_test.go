package functions_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/common/types/traits"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/oci/cel/functions"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestBindingToOCI_StringReference(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		expects map[string]string
		err     require.ErrorAssertionFunc
	}{
		{
			name:  "string reference with a version should succeed and set tag",
			input: "registry.io/myrepo/myapp:v1",
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "myrepo/myapp",
				"tag":        "v1",
				"digest":     "",
				"reference":  "v1",
			},
		},
		{
			name:  "string reference with a digest should succeed and set digest",
			input: "registry.io/myrepo/myapp@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "myrepo/myapp",
				"tag":        "",
				"digest":     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"reference":  "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
		},
		{
			name:  "string reference with a version & digest should succeed and set digest and tag",
			input: "registry.io/myrepo/myapp:v1@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "myrepo/myapp",
				"tag":        "v1",
				"digest":     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"reference":  "v1@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
		},
		{
			name:  "full reference pointing to another repo ignores resolver context",
			input: "registry.io/someotherrepo/myapp:v1@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "someotherrepo/myapp",
				"tag":        "v1",
				"digest":     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"reference":  "v1@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
		},
		{
			name:    "string reference with an invalid digest should fail",
			input:   "registry.io/myrepo/myapp:v1@sha256:gibberish",
			expects: map[string]string{},
			err:     require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runToOCITests(t, tc.input, nil, tc.expects, tc.err)
		})
	}
}

func TestBindingToOCI_TypedAccessSpecs(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		opts    []functions.Option
		expects map[string]string
		err     require.ErrorAssertionFunc
	}{
		{
			name: "OCIImage access with ociArtifact type",
			input: typedToMap(t, &ocispec.OCIImage{
				Type:           runtime.NewVersionedType("ociArtifact", "v1"),
				ImageReference: "ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0",
			}),
			expects: map[string]string{
				"host":       "ghcr.io",
				"registry":   "ghcr.io",
				"repository": "open-component-model/ocm/ocm.software/ocmcli/ocmcli-image",
				"tag":        "0.24.0",
				"digest":     "",
				"reference":  "0.24.0",
			},
		},
		{
			name: "OCIImage access with OCIImage/v1 type",
			input: typedToMap(t, &ocispec.OCIImage{
				Type:           runtime.NewVersionedType("OCIImage", "v1"),
				ImageReference: "registry.io/myrepo/myapp:v1@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			}),
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "myrepo/myapp",
				"tag":        "v1",
				"digest":     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"reference":  "v1@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
		},
		{
			name: "OCIImage access with OCIImage/v1 type pointing to another repo",
			input: typedToMap(t, &ocispec.OCIImage{
				Type:           runtime.NewVersionedType("OCIImage", "v1"),
				ImageReference: "registry.io/anotherrepo/myapp:v1@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			}),
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "anotherrepo/myapp",
				"tag":        "v1",
				"digest":     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"reference":  "v1@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
		},
		{
			name: "Helm access has no imageReference and returns error without resolver",
			input: map[string]any{
				"type":           "Helm/v1",
				"helmRepository": "oci://ghcr.io/org/charts",
				"helmChart":      "my-chart:1.0.0",
			},
			err: require.Error,
		},
		{
			name: "untyped map with imageReference falls back to deprecated path",
			input: map[string]any{
				"type":           "Helm/v1",
				"helmRepository": "oci://ghcr.io/org/charts",
				"helmChart":      "my-chart:1.0.0",
				"imageReference": "ghcr.io/org/charts/my-chart:1.0.0",
			},
			expects: map[string]string{
				"host":       "ghcr.io",
				"registry":   "ghcr.io",
				"repository": "org/charts/my-chart",
				"tag":        "1.0.0",
				"digest":     "",
				"reference":  "1.0.0",
			},
		},
		{
			name: "map without type returns error",
			input: map[string]any{
				"foo": "bar",
			},
			err: require.Error,
		},
		{
			name: "resolver handles unknown access type",
			input: map[string]any{
				"type": "custom/v1",
				"url":  "https://example.com",
			},
			opts: []functions.Option{
				functions.WithReferenceResolver(func(access *runtime.Raw) (functions.Reference, bool, error) {
					return functions.Reference{
						Host:       "example.com",
						Repository: "custom/repo",
						Tag:        "latest",
					}, true, nil
				}),
			},
			expects: map[string]string{
				"host":       "example.com",
				"registry":   "example.com",
				"repository": "custom/repo",
				"tag":        "latest",
				"digest":     "",
				"reference":  "latest",
			},
		},
		{
			name: "resolver returning ok=false falls through to deprecation fallback",
			input: map[string]any{
				"type":           "custom/v1",
				"imageReference": "ghcr.io/fallback/image:v2",
			},
			opts: []functions.Option{
				functions.WithReferenceResolver(func(access *runtime.Raw) (functions.Reference, bool, error) {
					return functions.Reference{}, false, nil
				}),
			},
			expects: map[string]string{
				"host":       "ghcr.io",
				"registry":   "ghcr.io",
				"repository": "fallback/image",
				"tag":        "v2",
				"digest":     "",
				"reference":  "v2",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runToOCITests(t, tc.input, tc.opts, tc.expects, tc.err)
		})
	}
}

func TestParseReference(t *testing.T) {
	r := require.New(t)

	ref, err := functions.ParseReference("ghcr.io/org/repo:v1")
	r.NoError(err)
	r.Equal("ghcr.io", ref.Host)
	r.Equal("org/repo", ref.Repository)
	r.Equal("v1", ref.Tag)
	r.Empty(ref.Digest)

	m := ref.Map()
	r.Equal("ghcr.io", m["host"])
	r.Equal("ghcr.io", m["registry"])
	r.Equal("org/repo", m["repository"])
	r.Equal("v1", m["tag"])
	r.Empty(m["digest"])
	r.Equal("v1", m["reference"])
}

// runToOCITests runs tests against the ToOCI CEL binding and asserts against the desired output.
func runToOCITests(t *testing.T,
	input any,
	opts []functions.Option,
	expects map[string]string,
	errFunc require.ErrorAssertionFunc,
) {
	t.Helper()
	r := require.New(t)
	bindFn := functions.BindingToOCI(opts...)

	var val ref.Val
	var celType *cel.Type
	switch v := input.(type) {
	case string:
		val = bindFn(types.String(v))
		celType = cel.StringType
	case map[string]any:
		val = bindFn(types.DefaultTypeAdapter.NativeToValue(v))
		celType = cel.DynType
	default:
		r.Failf("Unsupported input", "runToOCITests does not support: %v", v)
	}
	r.NotNil(val)

	if errFunc != nil {
		r.IsType(&types.Err{}, val)
		errFunc(t, val.(*types.Err))
		return
	}

	assertCelMap(t, val, expects)

	t.Run("cel", func(t *testing.T) {
		r := require.New(t)
		env, err := cel.NewEnv(functions.ToOCI(opts...), cel.Variable("value", celType))
		r.NoError(err)
		ast, issues := env.Compile(fmt.Sprintf("value.%s()", functions.ToOCIFunctionName))
		r.NoError(issues.Err())

		prog, err := env.Program(ast)
		r.NoError(err)
		val, _, err := prog.ContextEval(t.Context(), map[string]any{
			"value": input,
		})
		r.NoError(err)

		assertCelMap(t, val, expects)
	})
}

// assertCelMap checks if the evaluated CEL value matches the expected test data.
func assertCelMap(t *testing.T, val ref.Val, expects map[string]string) {
	t.Helper()
	r := require.New(t)
	mapper, ok := val.(traits.Mapper)
	r.True(ok, "expected traits.Mapper, got %T", val)
	a := assert.New(t)
	for k, v := range expects {
		a.EqualValues(v, mapper.Get(types.String(k)).Value())
	}
}

// typedToMap converts a runtime.Typed struct to map[string]any via JSON roundtrip,
// simulating how access specs arrive from component descriptors.
func typedToMap(t *testing.T, typed any) map[string]any {
	t.Helper()
	data, err := json.Marshal(typed)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	return m
}
