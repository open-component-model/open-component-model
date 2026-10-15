package v1_test

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// testdata holds known-answer fixtures transcribed from the cc-utils anchor
// (ocm/sign.py). Each case folder carries:
//   - input.yaml    the cd (readable YAML; equivalent to the exact cd.json)
//   - expected.json the cc-utils oracle normalised bytes (byte-exact)
//   - digest.txt    the cc-utils oracle sha256 of expected.json
//
// Do not recompute expected values with this normaliser — that would be circular.
//
//go:embed testdata
var testdata embed.FS

// loadDescriptor loads a case input.yaml into the runtime descriptor, mirroring
// the signing flow (v2.Descriptor → ConvertFromV2).
func loadDescriptor(t *testing.T, folder string) *descruntime.Descriptor {
	t.Helper()
	r := require.New(t)
	in, err := testdata.ReadFile(filepath.Join("testdata", folder, "input.yaml"))
	r.NoError(err)
	var desc v2.Descriptor
	r.NoError(yaml.Unmarshal(in, &desc))
	rt, err := descruntime.ConvertFromV2(&desc)
	r.NoError(err)
	return rt
}

// TestNormaliseByteExact asserts the normalised output is byte-identical to the
// cc-utils known-answer bytes and that sha256(output) matches the oracle digest.
func TestNormaliseByteExact(t *testing.T) {
	cases, err := testdata.ReadDir("testdata")
	require.NoError(t, err)
	require.NotEmpty(t, cases)

	for _, c := range cases {
		if !c.IsDir() {
			continue
		}
		t.Run(c.Name(), func(t *testing.T) {
			r := require.New(t)
			expected, err := testdata.ReadFile(filepath.Join("testdata", c.Name(), "expected.json"))
			r.NoError(err)
			digest, err := testdata.ReadFile(filepath.Join("testdata", c.Name(), "digest.txt"))
			r.NoError(err)

			desc := loadDescriptor(t, c.Name())
			got, err := normalisation.Normalise(desc, v1.Algorithm)
			r.NoError(err)
			r.Equal(string(expected), string(got), "normalised bytes diverge from cc-utils oracle")

			sum := sha256.Sum256(got)
			r.Equal(string(digest), hex.EncodeToString(sum[:]), "digest diverges from cc-utils oracle")
		})
	}
}

// TestUnknownAlgorithmErrors locks in that an unregistered normalisation
// algorithm still errors at the registry, so registering v1 did not weaken the
// unknown-algorithm guard.
func TestUnknownAlgorithmErrors(t *testing.T) {
	r := require.New(t)
	desc := loadDescriptor(t, "01-minimal-oci")
	_, err := normalisation.Normalise(desc, "jsonNormalisation/bogus")
	r.ErrorIs(err, normalisation.ErrUnknownNormalisationAlgorithm)
}
