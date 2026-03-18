package functions_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/traits"
	"github.com/google/cel-go/common/types/ref"
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

// componentInfoForRepository creates a ComponentInfo with a repository spec pointing to the given registry and optional subPath.
func componentInfoForRepository(repository string, subPath string) *v1alpha1.ComponentInfo {
	repoSpec := fmt.Sprintf(`{"type":"OCIRepository/v1","baseUrl":"https://%s","subPath":"%s"}`, repository, subPath)
	return &v1alpha1.ComponentInfo{
		RepositorySpec: &apiextensionsv1.JSON{Raw: []byte(repoSpec)},
		Component:      "test-component",
		Version:        "v1.0.0",
	}
}

// TODO(matthiasnbruns): we will drop support for this completely - https://github.com/open-component-model/ocm-project/issues/960
func TestBindingToOCI_StringReference(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		component *v1alpha1.ComponentInfo
		expects   map[string]string
		err       require.ErrorAssertionFunc
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
			name:  "full reference pointing to another repo",
			input: "registry.io/someotherrepo/myapp:v1@sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			// this is a theoretical test case that should not have any impact on the reference at all
			component: componentInfoForRepository("registry.io", "myrepo"),
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
			name:      "string reference with an invalid digest should fail",
			input:     "registry.io/myrepo/myapp:v1@sha256:gibberish",
			component: componentInfoForRepository("registry.io", "myrepo"),
			expects:   map[string]string{},
			err:       require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runToOCITests(t, tc.input, tc.component, tc.expects, tc.err)
		})
	}
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
			component: componentInfoForRepository("registry.io", "myrepo"),
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
			name: "localBlob builds reference from repo spec and component info",
			input: typedToMap(t, &v2.LocalBlob{
				Type:           runtime.NewVersionedType("localBlob", "v1"),
				LocalReference: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				MediaType:      "application/vnd.oci.image.manifest.v1+json",
			}),
			component: componentInfoForRepository("ghcr.io", "myrepo"),
			expects: map[string]string{
				"host":       "ghcr.io",
				"registry":   "ghcr.io",
				"repository": "myrepo/component-descriptors/test-component",
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
			component: componentInfoForRepository("ghcr.io", "org/charts"),
			err:       require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runToOCITests(t, tc.input, tc.component, tc.expects, tc.err)
		})
	}
}

// runToOCITests runs tests for a map input against the ToOCI cel binding and assets against the desired output
func runToOCITests(t *testing.T,
	input any,
	component *v1alpha1.ComponentInfo,
	expects map[string]string,
	errFunc require.ErrorAssertionFunc,
) {
	r := require.New(t)
	bindFn := functions.BindingToOCI(component)

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
		env, err := cel.NewEnv(functions.ToOCI(component), cel.Variable("value", celType))
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

// assertCelMap checks if the evaluated cel value matches the expected test data
func assertCelMap(t *testing.T, val ref.Val, expects map[string]string) {
	r := require.New(t)
	r.IsType(&lazy.MapValue{}, val)
	mv := val.(*lazy.MapValue)
	a := assert.New(t)
	for k, v := range expects {
		a.EqualValues(v, mv.Get(types.String(k)).Value(), "key %q", k)
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
