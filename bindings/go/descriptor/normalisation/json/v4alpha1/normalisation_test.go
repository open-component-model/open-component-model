package v4alpha1_test

import (
	"embed"
	"path/filepath"
	"strings"
	"testing"

	_ "embed"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

//go:embed testdata
var testdata embed.FS

func TestConformance(t *testing.T) {
	baseName := t.Name()
	prefix := filepath.Join("testdata", "conformance")

	run := func(t *testing.T, folder string) {
		t.Helper()
		r := require.New(t)
		desc, err := testdata.ReadFile(filepath.Join(prefix, folder, "README.md"))
		r.NoError(err, "failed to read test README")
		t.Log(string(desc))

		input, err := testdata.ReadFile(filepath.Join(prefix, folder, "input.yaml"))
		r.NoError(err, "failed to read test input")
		expected, err := testdata.ReadFile(filepath.Join(prefix, folder, "expected.json"))
		r.NoError(err, "failed to read test expected output")

		var descriptor descriptorv2.Descriptor
		if err := yaml.Unmarshal(input, &descriptor); err != nil {
			t.Fatalf("failed to unmarshal YAML: %v", err)
		}
		runtimeDescriptor, err := runtime.ConvertFromV2(&descriptor)
		r.NoError(err, "failed to convert descriptor")

		// Normalise the descriptor using our normalization (ExclusionRules applied).
		normalizedBytes, err := normalisation.Normalise(runtimeDescriptor, v4alpha1.Algorithm)
		r.NoError(err, "failed to normalise descriptor")

		r.Equal(string(expected), string(normalizedBytes),
			"normalized output does not match expected output from testcase",
		)
	}

	t.Run(filepath.Join("legacy", "jsonNormalisation", "v3"), func(t *testing.T) {
		prefix = filepath.Join(prefix, strings.TrimPrefix(t.Name(), baseName))
		tests, err := testdata.ReadDir(prefix)
		require.NoError(t, err, "failed to read conformance test directory")
		for _, folder := range tests {
			t.Run(folder.Name(), func(t *testing.T) {
				run(t, folder.Name())
			})
		}
	})
}

// TestNormalization verifies that the normalization (using ExclusionRules)
// produces the expected canonical JSON output.
func TestNormalization(t *testing.T) {
	// YAML input representing a component descriptor.
	inputYAML := `
component:
  componentReferences: null
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
	var descriptor descriptorv2.Descriptor
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
	expected := `{"component":{"componentReferences":[],"name":"github.com/vasu1124/introspect","provider":{"name":"internal"},"resources":[{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"6a1c7637a528ab5957ab60edf73b5298a0a03de02a96be0313ee89b22544840c"},"labels":[{"name":"label2","signing":true,"value":"bar"}],"name":"introspect-image","relation":"local","type":"ociImage","version":"1.0.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"genericBlobDigest/v1","value":"d1187ac17793b2f5fa26175c21cabb6ce388871ae989e16ff9a38bd6b32507bf"},"name":"introspect-blueprint","relation":"local","type":"landscaper.gardener.cloud/blueprint","version":"1.0.0"},{"digest":{"hashAlgorithm":"SHA-256","normalisationAlgorithm":"ociArtifactDigest/v1","value":"6229be2be7e328f74ba595d93b814b590b1aa262a1b85e49cc1492795a9e564c"},"name":"introspect-helm","relation":"external","type":"helm","version":"0.1.0"}],"sources":[{"name":"introspect","type":"git","version":"1.0.0"}],"version":"1.0.0"}}`

	assert.JSONEq(t, expected, normalized)
	if normalized != expected {
		t.Errorf("normalized output does not match expected.\nExpected:\n%s\nGot:\n%s", expected, normalized)
	}
}

// TestVersionedLabelExcluded pins jsonNormalisation/v4alpha1 to its historical
// bytes: a signed label's "version" must NOT appear in the normalised output,
// at every label position — including component references, which previously
// bypassed label filtering entirely (the rule was keyed "references" while the
// JSON key is "componentReferences"). The runtime<->v2 conversion started
// preserving Label.Version for jsonNormalisation/v1; v4alpha1 must stay
// byte-stable, or existing signatures over versioned signing labels would fail.
//
// It also pins the deliberately-unchanged behaviour: non-signing reference
// labels are still kept (v4alpha1 never filtered reference labels; proper
// signing-only filtering is deferred to v4alpha3). Only label.version is
// stripped from references.
func TestVersionedLabelExcluded(t *testing.T) {
	r := require.New(t)

	mk := func(ver string) *runtime.Descriptor {
		d := &runtime.Descriptor{}
		d.Component.Name = "acme.org/x"
		d.Component.Version = "1.0.0"
		d.Component.Provider.Name = "acme"
		d.Component.Labels = []runtime.Label{{Name: "k", Value: []byte(`"v"`), Signing: true, Version: ver}}
		d.Component.References = []runtime.Reference{{
			ElementMeta: runtime.ElementMeta{ObjectMeta: runtime.ObjectMeta{
				Name: "ref", Version: "1.0.0",
				Labels: []runtime.Label{
					{Name: "sig", Value: []byte(`"v"`), Signing: true, Version: ver},
					{Name: "nosig", Value: []byte(`"x"`), Signing: false, Version: ver},
				},
			}},
			Component: "acme.org/child",
		}}
		return d
	}

	withVer, err := normalisation.Normalise(mk("v1"), v4alpha1.Algorithm)
	r.NoError(err)
	noVer, err := normalisation.Normalise(mk(""), v4alpha1.Algorithm)
	r.NoError(err)

	// Label version must have no effect on v4alpha1 output, anywhere.
	r.Equal(string(noVer), string(withVer), "label version must not change v4alpha1 bytes")
	r.NotContains(string(withVer), `"version":"v1"`)
	// Component label: filtered to name/value/signing.
	r.Contains(string(withVer), `"labels":[{"name":"k","signing":true,"value":"v"}]`)
	// Reference labels: version stripped, but the non-signing label is still kept
	// (pre-v1 behaviour; full filtering is a v4alpha3 change).
	r.Contains(string(withVer), `"labels":[{"name":"sig","signing":true,"value":"v"},{"name":"nosig","value":"x"}]`)
}

func TestNormalise(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		excludes jcs.TransformationRules
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
		{
			// Regression test for #3431: label values may be arbitrary JSON,
			// including arrays (e.g. cloud.gardener.cnudie/responsibles). The
			// normalisation used during signing must preserve such values.
			name: "signed label with array value",
			input: []interface{}{
				map[string]interface{}{
					"name": "cloud.gardener.cnudie/responsibles",
					"value": []interface{}{
						map[string]interface{}{
							"github_hostname": "github.com",
							"teamname":        "open-component-model/odg-maintainers",
							"type":            "githubTeam",
						},
					},
					"signing": true,
				},
			},
			excludes: v4alpha1.LabelExcludes,
			expected: `[{"name":"cloud.gardener.cnudie/responsibles","signing":true,"value":[{"github_hostname":"github.com","teamname":"open-component-model/odg-maintainers","type":"githubTeam"}]}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jcs.Normalise(tt.input, tt.excludes)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.JSONEq(t, tt.expected, string(got))
		})
	}
}
