package functions_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apiserver/pkg/cel/lazy"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	helmspec "ocm.software/open-component-model/bindings/go/helm/access/spec/v1"
	ocispec "ocm.software/open-component-model/bindings/go/oci/spec/access/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
	"ocm.software/open-component-model/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/kubernetes/controller/internal/cel/functions"
)

// componentInfoForRegistry creates a ComponentInfo with a repository spec pointing to the given registry and optional subPath.
func componentInfoForRepository(registry string, subPath ...string) *v1alpha1.ComponentInfo {
	var repoSpec string
	if len(subPath) > 0 && subPath[0] != "" {
		repoSpec = fmt.Sprintf(`{"type":"OCIRepository/v1","baseUrl":"https://%s","subPath":"%s"}`, registry, subPath[0])
	} else {
		repoSpec = fmt.Sprintf(`{"type":"OCIRepository/v1","baseUrl":"https://%s"}`, registry)
	}
	return &v1alpha1.ComponentInfo{
		RepositorySpec: &apiextensionsv1.JSON{Raw: []byte(repoSpec)},
		Component:      "test-component",
		Version:        "v1.0.0",
	}
}

func TestBindingToOCI_StringReference(t *testing.T) {
	tests := []struct {
		input     string
		component *v1alpha1.ComponentInfo
		expects   map[string]string
		err       require.ErrorAssertionFunc
	}{
		{
			input:     "registry.io/myrepo/myapp:v1",
			component: componentInfoForRegistry("registry.io"),
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
			input:     "registry.io/myrepo/myapp@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			component: componentInfoForRegistry("registry.io"),
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
			input:     "registry.io/myrepo/myapp:v1@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			component: componentInfoForRegistry("registry.io"),
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
			input:     "registry.io/myrepo/myapp:v1@sha256:gibberish",
			component: componentInfoForRegistry("registry.io"),
			expects:   map[string]string{},
			err:       require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			r := require.New(t)
			bindFn := functions.BindingToOCI(tc.component)
			val := bindFn(types.String(tc.input))
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
				env, err := cel.NewEnv(functions.ToOCI(tc.component), cel.Variable("value", cel.StringType))
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
		name      string
		input     map[string]any
		component *v1alpha1.ComponentInfo
		expects   map[string]string
		err       require.ErrorAssertionFunc
	}{
		{
			name: "OCIImage access with imageReference",
			input: map[string]any{
				"type":           "OCIImage/v1",
				"imageReference": "registry.io/myrepo/myapp:v1",
			},
			component: componentInfoForRegistry("registry.io"),
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
			component: componentInfoForRegistry("ghcr.io", "open-component-model"),
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
			component: componentInfoForRegistry("registry.io"),
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
			component: componentInfoForRegistry("registry.io"),
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
			name: "localBlob builds reference from repo spec and component info",
			input: map[string]any{
				"type":           "localBlob/v1",
				"localReference": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"mediaType":      "application/vnd.oci.image.manifest.v1+json",
			},
			component: componentInfoForRegistry("registry.io", "my-org"),
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "my-org/component-descriptors/test-component",
				"tag":        "",
				"digest":     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"reference":  "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
		},
		{
			name: "localBlob without subPath",
			input: map[string]any{
				"type":           "localBlob/v1",
				"localReference": "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"mediaType":      "application/vnd.oci.image.manifest.v1+json",
			},
			component: componentInfoForRegistry("registry.io"),
			expects: map[string]string{
				"host":       "registry.io",
				"registry":   "registry.io",
				"repository": "component-descriptors/test-component",
				"tag":        "",
				"digest":     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"reference":  "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
		},
		{
			name: "backward compatible plain imageReference without type",
			input: map[string]any{
				"imageReference": "registry.io/myrepo/myapp:v1",
			},
			component: componentInfoForRegistry("registry.io"),
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
			component: componentInfoForRegistry("registry.io"),
			err:       require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			bindFn := functions.BindingToOCI(tc.component)
			val := bindFn(types.DefaultTypeAdapter.NativeToValue(tc.input))
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
				env, err := cel.NewEnv(functions.ToOCI(tc.component), cel.Variable("value", cel.DynType))
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
		name      string
		input     map[string]any
		component *v1alpha1.ComponentInfo
		expects   map[string]string
		err       require.ErrorAssertionFunc
	}{
		{
			name: "OCIImage access with ociArtifact type",
			input: typedToMap(t, &ocispec.OCIImage{
				Type:           runtime.NewVersionedType("ociArtifact", "v1"),
				ImageReference: "ghcr.io/open-component-model/ocm/ocm.software/ocmcli/ocmcli-image:0.24.0",
			}),
			component: componentInfoForRegistry("ghcr.io", "open-component-model/ocm"),
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
			component: componentInfoForRegistry("registry.io"),
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
			name: "localBlob builds reference from repo spec and component info",
			input: typedToMap(t, &v2.LocalBlob{
				Type:           runtime.NewVersionedType("localBlob", "v1"),
				LocalReference: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				MediaType:      "application/vnd.oci.image.manifest.v1+json",
			}),
			component: componentInfoForRegistry("ghcr.io", "org"),
			expects: map[string]string{
				"host":       "ghcr.io",
				"registry":   "ghcr.io",
				"repository": "org/component-descriptors/test-component",
				"tag":        "",
				"digest":     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"reference":  "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
		},
		{
			name: "Helm access has no imageReference and returns error",
			input: typedToMap(t, &helmspec.Helm{
				Type:           runtime.NewVersionedType("Helm", "v1"),
				HelmRepository: "oci://ghcr.io/org/charts",
				HelmChart:      "my-chart:1.0.0",
			}),
			component: componentInfoForRegistry("ghcr.io", "org/charts"),
			err:       require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			bindFn := functions.BindingToOCI(tc.component)
			val := bindFn(types.DefaultTypeAdapter.NativeToValue(tc.input))
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
				env, err := cel.NewEnv(functions.ToOCI(tc.component), cel.Variable("value", cel.DynType))
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
