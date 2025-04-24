package v4alpha1_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptor2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// TestNormalization verifies that the normalization (using ExclusionRules)
// produces the expected canonical JSON output.
func TestNormalization(t *testing.T) {
	// YAML input representing a component descriptor.
	inputYAML := `
component:
  componentReferences: []
  name: github.com/vasu1124/introspect
  provider: internal
  repositoryContexts:
  - baseUrl: ghcr.io/vasu1124/ocm
    componentNameMapping: urlPath
    type: ociRegistry
  resources:
  - access:
      localReference: sha256:7f0168496f273c1e2095703a050128114d339c580b0906cd124a93b66ae471e2
      mediaType: application/vnd.docker.distribution.manifest.v2+tar+gzip
      referenceName: vasu1124/introspect:1.0.0
      type: localBlob
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: 6a1c7637a528ab5957ab60edf73b5298a0a03de02a96be0313ee89b22544840c
    labels:
    - name: label1
      value: foo
    - name: label2
      value: bar
      signing: true
      mergeAlgorithm: test
    name: introspect-image
    relation: local
    type: ociImage
    version: 1.0.0
  - access:
      localReference: sha256:d1187ac17793b2f5fa26175c21cabb6ce388871ae989e16ff9a38bd6b32507bf
      mediaType: ""
      type: localBlob
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: genericBlobDigest/v1
      value: d1187ac17793b2f5fa26175c21cabb6ce388871ae989e16ff9a38bd6b32507bf
    name: introspect-blueprint
    relation: local
    type: landscaper.gardener.cloud/blueprint
    version: 1.0.0
  - access:
      localReference: sha256:4186663939459149a21c0bb1cd7b8ff86e0021b29ca45069446d046f808e6bfe
      mediaType: application/vnd.oci.image.manifest.v1+tar+gzip
      referenceName: vasu1124/helm/introspect-helm:0.1.0
      type: localBlob
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: ociArtifactDigest/v1
      value: 6229be2be7e328f74ba595d93b814b590b1aa262a1b85e49cc1492795a9e564c
    name: introspect-helm
    relation: external
    type: helm
    version: 0.1.0
  sources:
  - access:
      repository: github.com/vasu1124/introspect
      type: git
    name: introspect
    type: git
    version: 1.0.0
  version: 1.0.0
meta:
  schemaVersion: v2
`

	// Unmarshal YAML into a generic map.
	var descriptor descriptor2.Descriptor
	if err := yaml.Unmarshal([]byte(inputYAML), &descriptor); err != nil {
		t.Fatalf("failed to unmarshal YAML: %v", err)
	}

	desc, err := runtime.ConvertFromV2(&descriptor)
	if err != nil {
		t.Fatalf("failed to convert descriptor: %v", err)
	}

	// Normalise the descriptor using our normalization (ExclusionRules applied).
	normalizedBytes, err := normalisation.Normalise(desc, v4alpha1.Algorithm)
	if err != nil {
		t.Fatalf("failed to normalize descriptor: %v", err)
	}
	normalized := string(normalizedBytes)

	// Expected canonical JSON output.
	// Note: Fields that are excluded (e.g. "meta", "repositoryContexts", "access" in resources, etc.)
	// are omitted and maps/arrays are canonically ordered.
	expected := `{"component":{"componentReferences":[],"name":"github.com/vasu1124/introspect","provider":"internal","resources":[{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"6a1c7637a528ab5957ab60edf73b5298a0a03de02a96be0313ee89b22544840c"},"labels":[{"name":"label2","signing":true,"value":"bar"}],"name":"introspect-image","relation":"local","type":"ociImage","version":"1.0.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"d1187ac17793b2f5fa26175c21cabb6ce388871ae989e16ff9a38bd6b32507bf"},"name":"introspect-blueprint","relation":"local","type":"landscaper.gardener.cloud/blueprint","version":"1.0.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"6229be2be7e328f74ba595d93b814b590b1aa262a1b85e49cc1492795a9e564c"},"name":"introspect-helm","relation":"external","type":"helm","version":"0.1.0"}],"sources":[{"name":"introspect","type":"git","version":"1.0.0"}],"version":"1.0.0"}}`

	assert.JSONEq(t, expected, normalized)
	if normalized != expected {
		t.Errorf("normalized output does not match expected.\nExpected:\n%s\nGot:\n%s", expected, normalized)
	}
}

func TestMapResourcesWithAccessType(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name: "valid access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
			},
		},
		{
			name: "invalid access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123, // invalid type
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123,
				},
				"digest": "test",
			},
		},
		{
			name: "missing access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
		},
		{
			name: "missing access",
			input: map[string]interface{}{
				"digest": "test",
			},
			expected: map[string]interface{}{
				"digest": "test",
			},
		},
		{
			name: "nil access",
			input: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v4alpha1.MapResourcesWithAccessType(v4alpha1.IsNoneAccessKind, func(v interface{}) interface{} {
				m := v.(map[string]interface{})
				delete(m, "digest")
				return m
			}, tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestMapResourcesWithNoneAccess(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name: "valid none access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "none",
				},
			},
		},
		{
			name: "valid legacy none access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "None",
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": "None",
				},
			},
		},
		{
			name: "invalid access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123, // invalid type
				},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{
					"type": 123,
				},
				"digest": "test",
			},
		},
		{
			name: "missing access type",
			input: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": map[string]interface{}{},
				"digest": "test",
			},
		},
		{
			name: "missing access",
			input: map[string]interface{}{
				"digest": "test",
			},
			expected: map[string]interface{}{
				"digest": "test",
			},
		},
		{
			name: "nil access",
			input: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
			expected: map[string]interface{}{
				"access": nil,
				"digest": "test",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v4alpha1.MapResourcesWithNoneAccess(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestIgnoreLabelsWithoutSignature(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected bool
	}{
		{
			name: "with signature true",
			input: map[string]interface{}{
				"signing": true,
			},
			expected: false,
		},
		{
			name: "with signature string true",
			input: map[string]interface{}{
				"signing": "true",
			},
			expected: false,
		},
		{
			name: "with signature false",
			input: map[string]interface{}{
				"signing": false,
			},
			expected: true,
		},
		{
			name: "with signature string false",
			input: map[string]interface{}{
				"signing": "false",
			},
			expected: true,
		},
		{
			name: "without signature",
			input: map[string]interface{}{
				"other": "value",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := v4alpha1.IgnoreLabelsWithoutSignature(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestNormalise(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		excludes jcs.ExcludeRules
		expected string
		wantErr  bool
	}{
		{
			name: "labels with signature",
			input: []interface{}{
				map[string]interface{}{
					"name":    "test",
					"value":   "value",
					"signing": true,
				},
				map[string]interface{}{
					"name":    "test2",
					"value":   "value2",
					"signing": false,
				},
			},
			excludes: v4alpha1.LabelExcludes,
			expected: `[{"name":"test","signing":true,"value":"value"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jcs.Normalise(tt.input, tt.excludes)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(got))
		})
	}
}
