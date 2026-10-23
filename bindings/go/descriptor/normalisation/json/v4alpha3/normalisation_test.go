package v4alpha3_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha3"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	descriptorv2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// fromYAML loads a v2 YAML fixture into the runtime descriptor, mirroring the
// signing flow (v2.Descriptor → ConvertFromV2).
func fromYAML(t *testing.T, in string) *descruntime.Descriptor {
	t.Helper()
	r := require.New(t)
	var desc descriptorv2.Descriptor
	r.NoError(yaml.Unmarshal([]byte(in), &desc))
	rt, err := descruntime.ConvertFromV2(&desc)
	r.NoError(err)
	return rt
}

func normalise(t *testing.T, in, algo string) string {
	t.Helper()
	out, err := normalisation.Normalise(fromYAML(t, in), algo)
	require.NoError(t, err)
	return string(out)
}

// TestReconvergesWithV3 is the byte-exact known-answer check against the
// independent legacy jsonNormalisation/v3 oracle. Where v3 already behaved
// correctly — no reference labels — v4alpha3 must reproduce its bytes and digest
// exactly, including a signed label.version.
func TestReconvergesWithV3(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantNorm string
		wantHex  string
	}{
		{"delta-free", deltaFreeInput, deltaFreeV3Norm, deltaFreeV3Digest},
		{"version-labels", versionLabelsInput, versionLabelsV3Norm, versionLabelsV3Digest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got := normalise(t, tt.input, v4alpha3.Algorithm)
			r.Equal(tt.wantNorm, got, "v4alpha3 must reproduce the legacy v3 bytes")
			sum := sha256.Sum256([]byte(got))
			r.Equal(tt.wantHex, hex.EncodeToString(sum[:]), "v4alpha3 digest must match the v3 oracle digest")
		})
	}
}

// TestSameDigestAsV4alpha1 pins the "both yield the same digest" scenario: with
// neither delta present, v4alpha1 and v4alpha3 produce identical bytes (and the
// same bytes as the v3 oracle).
func TestSameDigestAsV4alpha1(t *testing.T) {
	r := require.New(t)
	v1out := normalise(t, deltaFreeInput, v4alpha1.Algorithm)
	v2out := normalise(t, deltaFreeInput, v4alpha3.Algorithm)
	r.Equal(v1out, v2out, "delta-free input must normalise identically under v4alpha1 and v4alpha3")
	r.Equal(deltaFreeV3Norm, v2out, "and identically to legacy v3") //nolint:testifylint // byte-exact comparison, JSONEq would mask encoding differences
}

// TestVersionSignedDivergesFromV4alpha1 proves fix #1: a signing label's version
// is covered by v4alpha3 but not v4alpha1, so the bytes (and digest) differ and
// the version appears only in the v4alpha3 output.
func TestVersionSignedDivergesFromV4alpha1(t *testing.T) {
	r := require.New(t)
	v1out := normalise(t, versionLabelsInput, v4alpha1.Algorithm)
	v2out := normalise(t, versionLabelsInput, v4alpha3.Algorithm)

	r.NotEqual(v1out, v2out, "label version must change the signed bytes vs v4alpha1")
	r.NotContains(v1out, `"version":"v1"`, "v4alpha1 must not sign label.version")
	r.Contains(v2out, `"version":"v1"`, "v4alpha3 must sign component label.version")
	r.Contains(v2out, `"version":"v3"`, "v4alpha3 must sign resource label.version")
	r.Contains(v2out, `"version":"v4"`, "v4alpha3 must sign source label.version")
}

// TestReferenceFilteringDivergesFromV3 proves fix #2: reference labels are
// filtered signing-only under the correct componentReferences key. The
// non-signing reference label — which both legacy v3 AND v4alpha1 signed — is
// dropped, so v4alpha3 diverges from both while keeping the signing label's
// version.
func TestReferenceFilteringDivergesFromV3(t *testing.T) {
	r := require.New(t)
	v2out := normalise(t, referenceLabelsInput, v4alpha3.Algorithm)
	v1out := normalise(t, referenceLabelsInput, v4alpha1.Algorithm)

	r.Equal(referenceLabelsV4alpha2Norm, v2out, "v4alpha3 reference labels must be filtered signing-only, version kept") //nolint:testifylint // byte-exact comparison, JSONEq would mask encoding differences
	r.NotEqual(referenceLabelsV3Norm, v2out, "v4alpha3 must diverge from v3's unfiltered reference labels")
	r.NotEqual(v1out, v2out, "v4alpha3 must diverge from v4alpha1's reference-label handling")

	// The non-signing reference label leaks in both v3 and v4alpha1, and is gone
	// in v4alpha3.
	r.Contains(referenceLabelsV3Norm, `"name":"ref-nosig"`, "v3 leaks the non-signing reference label")
	r.Contains(v1out, `"name":"ref-nosig"`, "v4alpha1 leaks the non-signing reference label")
	r.NotContains(v2out, `"name":"ref-nosig"`, "v4alpha3 drops the non-signing reference label")
}
