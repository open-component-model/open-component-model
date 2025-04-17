package v3_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v3 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v3"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptor2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// TestNormalizationV3 verifies that the normalization (using CDExcludes)
// produces the expected canonical JSON output.
func TestNormalizationV3(t *testing.T) {
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

	// Normalise the descriptor using our v3 normalization (CDExcludes applied).
	normalizedBytes, err := normalisation.Normalise(desc, v3.Algorithm)
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
