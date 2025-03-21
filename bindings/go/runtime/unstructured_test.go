package runtime_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestUnstructured(t *testing.T) {
	testCases := []struct {
		name               string
		data               []byte
		un                 func() *runtime.Unstructured
		assertError        func(t *testing.T, err error)
		assertUnstructured func(t *testing.T, un *runtime.Unstructured)
		assertResult       func(t *testing.T, data []byte)
	}{
		{
			name: "successful unmarshal",
			data: []byte(`{
	"baseUrl": "ghcr.io",
	"componentNameMapping": "urlPath",
	"subPath": "open-component-model/ocm",
	"type": "OCIRegistry"
}`),
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			assertUnstructured: func(t *testing.T, un *runtime.Unstructured) {
				assert.Equal(t, "OCIRepository", un.GetType())
				value, ok := runtime.Get[string](un, "componentNameMapping")
				require.True(t, ok)
				assert.Equal(t, "OCIRepository", value)
			},
		},
		{
			name: "successful marshal",
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			un: func() *runtime.Unstructured {
				return &runtime.Unstructured{
					Data: map[string]interface{}{
						"componentNameMapping": "urlPath",
						"subPath":              "open-component-model/ocm",
						"type":                 "OCIRegistry",
						"baseUrl":              "ghcr.io",
					},
				}
			},
			// comparing string so if there is a conflict it's easier to see
			assertResult: func(t *testing.T, data []byte) {
				assert.Equal(t, "{\"baseUrl\":\"ghcr.io\",\"componentNameMapping\":\"urlPath\",\"subPath\":\"open-component-model/ocm\",\"type\":\"OCIRegistry\"}", string(data))
			},
		},
		{
			name: "set type",
			assertError: func(t *testing.T, err error) {
				require.NoError(t, err)
			},
			un: func() *runtime.Unstructured {
				un := runtime.Unstructured{
					Data: map[string]interface{}{
						"componentNameMapping": "urlPath",
					},
				}
				un.SetType(runtime.NewType("group", "version", "name"))
				return &un
			},
			// comparing string so if there is a conflict it's easier to see
			assertResult: func(t *testing.T, data []byte) {
				assert.Equal(t, "{\"componentNameMapping\":\"urlPath\",\"type\":\"group.name/version\"}", string(data))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Log("TestUnstructured:", tc.name)
			if tc.un != nil {
				un := tc.un()
				data, err := un.MarshalJSON()
				tc.assertError(t, err)
				tc.assertResult(t, data)
			} else {
				un := runtime.NewUnstructured()
				tc.assertError(t, un.UnmarshalJSON(tc.data))
			}
		})
	}
}
