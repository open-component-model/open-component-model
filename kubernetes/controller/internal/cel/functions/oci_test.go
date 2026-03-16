package functions_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apiserver/pkg/cel/lazy"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmspec "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/internal/cel/functions"
)

func TestBindingToOCI_StringReference(t *testing.T) {
	tests := []struct {
		input   string
		expects map[string]string
		err     require.ErrorAssertionFunc
	}{
		{
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
			input:   "registry.io/myrepo/myapp:v1@sha256:gibberish",
			expects: map[string]string{},
			err:     require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			r := require.New(t)
			val := functions.BindingToOCI(types.String(tc.input))
			r.NotNil(val)
			if tc.err != nil {
				r.IsType(&types.Err{}, val)
				tc.err(t, val.(*types.Err))
				return
			}

			r.IsType(&lazy.MapValue{}, val)
			mv := val.(*lazy.MapValue)
			a := assert.New(t)
			for k, v := range tc.expects {
				a.EqualValues(v, mv.Get(types.String(k)).Value())
			}

			t.Run("cel", func(t *testing.T) {
				r := require.New(t)
				env, err := cel.NewEnv(functions.ToOCI(), cel.Variable("value", cel.StringType))
				r.NoError(err)
				ast, issues := env.Compile(fmt.Sprintf("value.%s()", functions.ToOCIFunctionName))
				r.NoError(issues.Err())

				prog, err := env.Program(ast)
				r.NoError(err)
				val, _, err := prog.ContextEval(t.Context(), map[string]any{
					"value": tc.input,
				})
				if tc.err != nil {
					r.IsType(&types.Err{}, val)
					tc.err(t, val.(*types.Err))
					return
				}

				r.IsType(&lazy.MapValue{}, val)
				mv := val.(*lazy.MapValue)
				a := assert.New(t)
				for k, v := range tc.expects {
					a.EqualValues(v, mv.Get(types.String(k)).Value())
				}
			})
		})
	}
}

func TestBindingToOCI_MapReference(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
		expects map[string]string
		err     require.ErrorAssertionFunc
	}{
		{
			name: "OCIImage access with imageReference",
			input: map[string]any{
				"type":           "OCIImage/v1",
				"imageReference": "registry.io/myrepo/myapp:v1",
			},
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
			name: "OCIImage access with legacy ociArtifact type",
			input: map[string]any{
				"type":           "ociArtifact/v1",
				"imageReference": "ghcr.io/open-component-model/image:latest",
			},
			expects: map[string]string{
				"host":       "ghcr.io",
				"registry":   "ghcr.io",
				"repository": "open-component-model/image",
				"tag":        "latest",
				"digest":     "",
				"reference":  "latest",
			},
		},
		{
			name: "OCIImage access with digest",
			input: map[string]any{
				"type":           "OCIImage/v1",
				"imageReference": "registry.io/myrepo/myapp@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
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
			name: "OCIImage access with tag and digest",
			input: map[string]any{
				"type":           "OCIImage/v1",
				"imageReference": "registry.io/myrepo/myapp:v2@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "myrepo/myapp",
				"tag":        "v2",
				"digest":     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"reference":  "v2@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
		},
		{
			name: "localBlob with globalAccess containing OCIImage",
			input: map[string]any{
				"type":           "localBlob/v1",
				"localReference": "sha256:abc123",
				"mediaType":      "application/vnd.oci.image.manifest.v1+json",
				"globalAccess": map[string]any{
					"type":           "OCIImage/v1",
					"imageReference": "registry.io/global/image:v3",
				},
			},
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "global/image",
				"tag":        "v3",
				"digest":     "",
				"reference":  "v3",
			},
		},
		{
			name: "localBlob with globalAccess using legacy ociArtifact type",
			input: map[string]any{
				"type":           "localBlob/v1",
				"localReference": "sha256:abc123",
				"mediaType":      "application/vnd.oci.image.manifest.v1+json",
				"globalAccess": map[string]any{
					"type":           "ociArtifact/v1",
					"imageReference": "ghcr.io/org/chart:0.1.0",
				},
			},
			expects: map[string]string{
				"host":       "ghcr.io",
				"registry":   "ghcr.io",
				"repository": "org/chart",
				"tag":        "0.1.0",
				"digest":     "",
				"reference":  "0.1.0",
			},
		},
		{
			name: "localBlob prefers globalAccess over referenceName",
			input: map[string]any{
				"type":           "localBlob/v1",
				"localReference": "sha256:abc123",
				"mediaType":      "application/vnd.oci.image.manifest.v1+json",
				"referenceName":  "registry.io/fallback/image:v4",
				"globalAccess": map[string]any{
					"type":           "OCIImage/v1",
					"imageReference": "registry.io/preferred/image:v5",
				},
			},
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "preferred/image",
				"tag":        "v5",
				"digest":     "",
				"reference":  "v5",
			},
		},
		{
			name: "backward compatible plain imageReference without type",
			input: map[string]any{
				"imageReference": "registry.io/myrepo/myapp:v1",
			},
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
			name: "map without recognized access keys returns error",
			input: map[string]any{
				"type":      "someUnknownType/v1",
				"something": "else",
			},
			err: require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			val := functions.BindingToOCI(types.DefaultTypeAdapter.NativeToValue(tc.input))
			r.NotNil(val)
			if tc.err != nil {
				r.IsType(&types.Err{}, val)
				tc.err(t, val.(*types.Err))
				return
			}

			r.IsType(&lazy.MapValue{}, val, "expected lazy.MapValue but got %T: %v", val, val)
			mv := val.(*lazy.MapValue)
			a := assert.New(t)
			for k, v := range tc.expects {
				a.EqualValues(v, mv.Get(types.String(k)).Value(), "key %q", k)
			}

			t.Run("cel", func(t *testing.T) {
				r := require.New(t)
				env, err := cel.NewEnv(functions.ToOCI(), cel.Variable("value", cel.DynType))
				r.NoError(err)
				ast, issues := env.Compile(fmt.Sprintf("value.%s()", functions.ToOCIFunctionName))
				r.NoError(issues.Err())

				prog, err := env.Program(ast)
				r.NoError(err)
				val, _, err := prog.ContextEval(t.Context(), map[string]any{
					"value": tc.input,
				})
				if tc.err != nil {
					r.Error(err)
					return
				}
				r.NoError(err)

				r.IsType(&lazy.MapValue{}, val)
				mv := val.(*lazy.MapValue)
				a := assert.New(t)
				for k, v := range tc.expects {
					a.EqualValues(v, mv.Get(types.String(k)).Value(), "key %q", k)
				}
			})
		})
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

func TestBindingToOCI_TypedAccessSpecs(t *testing.T) {
	tests := []struct {
		name    string
		input   map[string]any
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
			name: "localBlob with globalAccess OCIImage",
			input: typedToMap(t, &v2.LocalBlob{
				Type:           runtime.NewVersionedType("localBlob", "v1"),
				LocalReference: "sha256:abc123",
				MediaType:      "application/vnd.oci.image.manifest.v1+json",
				GlobalAccess: &runtime.Raw{
					Type: runtime.NewVersionedType("OCIImage", "v1"),
					Data: []byte(`{"type":"OCIImage/v1","imageReference":"ghcr.io/org/component/image:2.0.0"}`),
				},
			}),
			expects: map[string]string{
				"host":       "ghcr.io",
				"registry":   "ghcr.io",
				"repository": "org/component/image",
				"tag":        "2.0.0",
				"digest":     "",
				"reference":  "2.0.0",
			},
		},
		{
			name: "Helm access has no imageReference and returns error",
			input: typedToMap(t, &helmspec.Helm{
				Type:           runtime.NewVersionedType("Helm", "v1"),
				HelmRepository: "oci://ghcr.io/org/charts",
				HelmChart:      "my-chart:1.0.0",
			}),
			err: require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			val := functions.BindingToOCI(types.DefaultTypeAdapter.NativeToValue(tc.input))
			r.NotNil(val)
			if tc.err != nil {
				r.IsType(&types.Err{}, val)
				tc.err(t, val.(*types.Err))
				return
			}

			r.IsType(&lazy.MapValue{}, val, "expected lazy.MapValue but got %T: %v", val, val)
			mv := val.(*lazy.MapValue)
			a := assert.New(t)
			for k, v := range tc.expects {
				a.EqualValues(v, mv.Get(types.String(k)).Value(), "key %q", k)
			}

			t.Run("cel", func(t *testing.T) {
				r := require.New(t)
				env, err := cel.NewEnv(functions.ToOCI(), cel.Variable("value", cel.DynType))
				r.NoError(err)
				ast, issues := env.Compile(fmt.Sprintf("value.%s()", functions.ToOCIFunctionName))
				r.NoError(issues.Err())

				prog, err := env.Program(ast)
				r.NoError(err)
				val, _, err := prog.ContextEval(t.Context(), map[string]any{
					"value": tc.input,
				})
				if tc.err != nil {
					r.Error(err)
					return
				}
				r.NoError(err)

				r.IsType(&lazy.MapValue{}, val)
				mv := val.(*lazy.MapValue)
				a := assert.New(t)
				for k, v := range tc.expects {
					a.EqualValues(v, mv.Get(types.String(k)).Value(), "key %q", k)
				}
			})
		})
	}
}
