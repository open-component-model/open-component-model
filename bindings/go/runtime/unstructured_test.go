package runtime_test

import (
	"encoding/json"
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
				un.SetType(runtime.NewVersionedType("name", "version"))
				return &un
			},
			// comparing string so if there is a conflict it's easier to see
			assertResult: func(t *testing.T, data []byte) {
				assert.Equal(t, "{\"componentNameMapping\":\"urlPath\",\"type\":\"name/version\"}", string(data))
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

type testDigest struct {
	HashAlgorithm          string `json:"hashAlgorithm"`
	NormalisationAlgorithm string `json:"normalisationAlgorithm"`
	Value                  string `json:"value"`
}

type testLabel struct {
	Name  string          `json:"name"`
	Value json.RawMessage `json:"value"`
}

type testResource struct {
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Labels        []testLabel       `json:"labels,omitempty"`
	ExtraIdentity map[string]string `json:"extraIdentity,omitempty"`
	Type          string            `json:"type"`
	Relation      string            `json:"relation"`
	Access        json.RawMessage   `json:"access"`
	Digest        *testDigest       `json:"digest,omitempty"`
}

func TestUnstructured_DeepCopyWithStructValues(t *testing.T) {
	resource := testResource{
		Name:    "test-resource",
		Version: "1.0.0",
		Labels: []testLabel{
			{Name: "env", Value: json.RawMessage(`"production"`)},
		},
		ExtraIdentity: map[string]string{
			"platform": "linux/amd64",
		},
		Type:     "ociImage",
		Relation: "external",
		Access:   json.RawMessage(`{"imageReference":"ghcr.io/test/image:v1.0.0","type":"ociArtifact"}`),
		Digest: &testDigest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: "ociArtifactDigest/v1",
			Value:                  "abc123",
		},
	}

	// Simulate an Unstructured whose Data contains a struct value (not a pure JSON type).
	// This can happen when values are set programmatically rather than via json.Unmarshal.
	un := &runtime.Unstructured{
		Data: map[string]interface{}{
			"resource": resource,
		},
	}

	// DeepCopy should normalize the struct through JSON marshal/unmarshal
	// so DeepCopyJSON does not panic on non-JSON-native types.
	copied := un.DeepCopy()
	require.NotNil(t, copied)

	// The copied resource should be a map[string]interface{} after normalization.
	copiedResource, ok := copied.Data["resource"].(map[string]interface{})
	require.True(t, ok, "expected map[string]interface{}, got %T", copied.Data["resource"])

	// Verify content matches the original struct's JSON representation.
	originalJSON, err := json.Marshal(resource)
	require.NoError(t, err)

	copiedJSON, err := json.Marshal(copiedResource)
	require.NoError(t, err)

	assert.JSONEq(t, string(originalJSON), string(copiedJSON))

	// Verify the copy is independent from the original.
	copiedResource["type"] = "modified"
	assert.Equal(t, "ociImage", resource.Type)
}

// deepCopyOriginal is the original DeepCopy implementation
func deepCopyOriginal(u *runtime.Unstructured) *runtime.Unstructured {
	if u == nil {
		return nil
	}
	out := new(runtime.Unstructured)
	*out = *u

	out.Data = runtime.DeepCopyJSON(u.Data)
	return out
}

// deepCopyFullMarshal is a version of DeepCopy that fully marshals/unmarshals the entire Data map, not just the normalized version.
// marshals/unmarshals the entire Data map, used as a baseline in benchmarks.
func deepCopyFullMarshal(u *runtime.Unstructured) *runtime.Unstructured {
	if u == nil {
		return nil
	}
	data, err := json.Marshal(u.Data)
	if err != nil {
		panic("deep copy: " + err.Error())
	}
	normalized := make(map[string]interface{})
	if err := json.Unmarshal(data, &normalized); err != nil {
		panic("deep copy: " + err.Error())
	}
	return &runtime.Unstructured{
		Data: runtime.DeepCopyJSON(normalized),
	}
}

// pureJSONData returns an Unstructured with only JSON-native types (the common case).
func pureJSONData() *runtime.Unstructured {
	return &runtime.Unstructured{
		Data: map[string]interface{}{
			"name":    "test-resource",
			"version": "1.0.0",
			"type":    "ociImage",
			"labels": []interface{}{
				map[string]interface{}{
					"name":  "env",
					"value": "production",
				},
			},
			"access": map[string]interface{}{
				"type":           "ociArtifact",
				"imageReference": "ghcr.io/test/image:v1.0.0",
			},
			"digest": map[string]interface{}{
				"hashAlgorithm":          "SHA-256",
				"normalisationAlgorithm": "ociArtifactDigest/v1",
				"value":                  "abc123",
			},
			"relation": "external",
			"extraIdentity": map[string]interface{}{
				"platform": "linux/amd64",
			},
		},
	}
}

// mixedData returns an Unstructured with a struct value that needs normalization.
func mixedData() *runtime.Unstructured {
	return &runtime.Unstructured{
		Data: map[string]interface{}{
			"name":    "test-resource",
			"version": "1.0.0",
			"type":    "ociImage",
			"resource": testResource{
				Name:     "nested-resource",
				Version:  "2.0.0",
				Type:     "ociImage",
				Relation: "external",
				Access:   json.RawMessage(`{"type":"ociArtifact","imageReference":"ghcr.io/test/image:v2.0.0"}`),
				Digest: &testDigest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: "ociArtifactDigest/v1",
					Value:                  "def456",
				},
			},
		},
	}
}

func BenchmarkDeepCopy_PureJSON_New(b *testing.B) {
	un := pureJSONData()
	b.ResetTimer()
	for b.Loop() {
		un.DeepCopy()
	}
}

func BenchmarkDeepCopy_PureJSON_Original(b *testing.B) {
	un := pureJSONData()
	b.ResetTimer()
	for b.Loop() {
		deepCopyOriginal(un)
	}
}

func BenchmarkDeepCopy_PureJSON_FullMarshal(b *testing.B) {
	un := pureJSONData()
	b.ResetTimer()
	for b.Loop() {
		deepCopyFullMarshal(un)
	}
}

func BenchmarkDeepCopy_MixedData_New(b *testing.B) {
	un := mixedData()
	b.ResetTimer()
	for b.Loop() {
		un.DeepCopy()
	}
}

func BenchmarkDeepCopy_MixedData_Original(b *testing.B) {
	defer func() {
		if r := recover(); r != nil {
			b.Skip("DeepCopy panics on mixed data, skipping benchmark. This is expected behavior since the original DeepCopy does not normalize non-JSON-native types.")
		}
	}()
	un := mixedData()
	b.ResetTimer()
	for b.Loop() {
		deepCopyOriginal(un)
	}
}

func BenchmarkDeepCopy_MixedData_FullMarshal(b *testing.B) {
	un := mixedData()
	b.ResetTimer()
	for b.Loop() {
		deepCopyFullMarshal(un)
	}
}
