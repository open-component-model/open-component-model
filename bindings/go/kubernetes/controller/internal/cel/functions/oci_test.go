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
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/api/v1alpha1"
	"ocm.software/open-component-model/bindings/go/kubernetes/controller/internal/cel/functions"
	ocifunctions "ocm.software/open-component-model/bindings/go/oci/cel/functions"
	"ocm.software/open-component-model/bindings/go/runtime"
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

func TestLocalBlobResolver(t *testing.T) {
	tests := []struct {
		name      string
		input     map[string]any
		component *v1alpha1.ComponentInfo
		expects   map[string]string
		err       require.ErrorAssertionFunc
	}{
		{
			name:      "localBlob builds reference from repo spec and component info",
			input:     typedToMap(t, &v2.LocalBlob{
				Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
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
			name:      "localBlob builds reference without subPath",
			input:     typedToMap(t, &v2.LocalBlob{
				Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
				LocalReference: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				MediaType:      "application/vnd.oci.image.manifest.v1+json",
			}),
			component: componentInfoForRepository("ghcr.io", ""),
			expects: map[string]string{
				"host":       "ghcr.io",
				"registry":   "ghcr.io",
				"repository": "component-descriptors/test-component",
				"tag":        "",
				"digest":     "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				"reference":  "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
			},
		},
		{
			name:      "localBlob with nil component returns error",
			input:     typedToMap(t, &v2.LocalBlob{
				Type:           runtime.NewVersionedType(v2.LocalBlobAccessType, v2.LocalBlobAccessTypeVersion),
				LocalReference: "sha256:9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
				MediaType:      "application/vnd.oci.image.manifest.v1+json",
			}),
			component: nil,
			err:       require.Error,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runToOCITests(t, tc.input, tc.component, tc.expects, tc.err)
		})
	}
}

// runToOCITests runs tests for a map input against the ToOCI CEL binding with the
// LocalBlobResolver and asserts against the desired output.
func runToOCITests(t *testing.T,
	input map[string]any,
	component *v1alpha1.ComponentInfo,
	expects map[string]string,
	errFunc require.ErrorAssertionFunc,
) {
	t.Helper()
	r := require.New(t)

	opts := []ocifunctions.Option{
		ocifunctions.WithReferenceResolver(functions.LocalBlobResolver(component)),
	}
	bindFn := ocifunctions.BindingToOCI(opts...)

	val := bindFn(types.DefaultTypeAdapter.NativeToValue(input))
	r.NotNil(val)

	if errFunc != nil {
		r.IsType(&types.Err{}, val)
		errFunc(t, val.(*types.Err))
		return
	}

	assertCelMap(t, val, expects)

	t.Run("cel", func(t *testing.T) {
		r := require.New(t)
		env, err := cel.NewEnv(ocifunctions.ToOCI(opts...), cel.Variable("value", cel.DynType))
		r.NoError(err)
		ast, issues := env.Compile(fmt.Sprintf("value.%s()", ocifunctions.ToOCIFunctionName))
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
