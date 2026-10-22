package v4alpha2_test

import (
	"embed"
	"path/filepath"
	"strings"
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

// TestConformance pins v4alpha2 to real jsonNormalisation/v3 output. The fixture
// is the actual `ocm hash cv --normalization jsonNormalisation/v3 -onorm` result
// for a component without versioned or reference labels, where v4alpha2's two
// deltas do not fire — so v4alpha2 must reproduce the legacy v3 bytes exactly.
// This is the "re-converges with v3" guarantee.
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

		normalizedBytes, err := normalisation.Normalise(runtimeDescriptor, v4alpha2.Algorithm)
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

// mkDescriptor builds a descriptor with a signing label (version ver) at the
// component position and, on a reference, one signing and one non-signing label
// (both version ver). It is the shared input for the two delta tests.
func mkDescriptor(ver string) *runtime.Descriptor {
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

// TestVersionSigned is the counterpart to v4alpha1's TestVersionedLabelExcluded:
// label.version IS part of the signed bytes, so it both appears in the output and
// changes the bytes when present.
func TestVersionSigned(t *testing.T) {
	r := require.New(t)

	withVer, err := normalisation.Normalise(mkDescriptor("v1"), v4alpha2.Algorithm)
	r.NoError(err)
	noVer, err := normalisation.Normalise(mkDescriptor(""), v4alpha2.Algorithm)
	r.NoError(err)

	// version is signed: it shows up and its presence changes the bytes.
	r.Contains(string(withVer), `"version":"v1"`)
	r.NotEqual(string(noVer), string(withVer), "label version must change v4alpha2 bytes")
	// Component label keeps name/signing/value/version (JCS orders keys).
	r.Contains(string(withVer), `"labels":[{"name":"k","signing":true,"value":"v","version":"v1"}]`)
}

// TestReferenceParity pins the reference-label fix: a reference label is filtered
// exactly like a component label — the signing label is kept (with version) and
// the non-signing label is dropped. v4alpha1 kept the non-signing reference label.
func TestReferenceParity(t *testing.T) {
	r := require.New(t)

	out, err := normalisation.Normalise(mkDescriptor("v1"), v4alpha2.Algorithm)
	r.NoError(err)
	s := string(out)

	// Only the signing reference label survives, carrying its version.
	r.Contains(s, `"labels":[{"name":"sig","signing":true,"value":"v","version":"v1"}]`)
	// The non-signing reference label is gone (v4alpha1 leak closed).
	r.NotContains(s, `"name":"nosig"`)
}
