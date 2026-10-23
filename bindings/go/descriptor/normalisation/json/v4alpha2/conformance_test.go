package v4alpha2_test

import (
	"embed"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha2"
	"ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

//go:embed testdata
var testdata embed.FS

// TestConformance pins v4alpha2 against the same real legacy jsonNormalisation/v3
// descriptor fixtures used by v4alpha1. Because v4alpha2 is the v3 alias target
// (signing.LegacyNormalisationAlgo resolves here), its output must reproduce the
// recorded v3 oracle byte-for-byte. The 01 fixture carries only non-signing
// labels and no reference labels, so v4alpha1, v4alpha2 and v3 all agree — this
// locks that the alias target stays faithful to a real-world v3 component.
func TestConformance(t *testing.T) {
	baseName := t.Name()
	prefix := filepath.Join("testdata", "conformance", "legacy", "jsonNormalisation", "v3")

	tests, err := testdata.ReadDir(prefix)
	require.NoError(t, err, "failed to read conformance test directory")
	for _, folder := range tests {
		t.Run(filepath.Join(baseName, folder.Name()), func(t *testing.T) {
			r := require.New(t)

			readme, err := testdata.ReadFile(filepath.Join(prefix, folder.Name(), "README.md"))
			r.NoError(err, "failed to read test README")
			t.Log(string(readme))

			input, err := testdata.ReadFile(filepath.Join(prefix, folder.Name(), "input.yaml"))
			r.NoError(err, "failed to read test input")
			expected, err := testdata.ReadFile(filepath.Join(prefix, folder.Name(), "expected.json"))
			r.NoError(err, "failed to read test expected output")

			var desc descriptorv2.Descriptor
			r.NoError(yaml.Unmarshal(input, &desc), "failed to unmarshal YAML")
			rt, err := runtime.ConvertFromV2(&desc)
			r.NoError(err, "failed to convert descriptor")

			got, err := normalisation.Normalise(rt, v4alpha2.Algorithm)
			r.NoError(err, "failed to normalise descriptor")

			r.Equal(string(expected), string(got), //nolint:testifylint // byte-exact comparison, JSONEq would mask encoding differences
				"v4alpha2 must reproduce the legacy v3 oracle byte-for-byte")
		})
	}
}
