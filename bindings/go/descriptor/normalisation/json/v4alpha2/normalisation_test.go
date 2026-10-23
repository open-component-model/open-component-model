package v4alpha2_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha2"
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

// TestIsLegacyV3 is the byte-exact known-answer check against the independent
// legacy jsonNormalisation/v3 oracle. v4alpha2 is the faithful v3
// re-implementation, so it must reproduce the oracle bytes and digest exactly
// for EVERY fixture — including the reference-labels case, where v3 keeps both
// labels unfiltered and signs label.version.
func TestIsLegacyV3(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantNorm string
		wantHex  string
	}{
		{"delta-free", deltaFreeInput, deltaFreeV3Norm, deltaFreeV3Digest},
		{"version-labels", versionLabelsInput, versionLabelsV3Norm, versionLabelsV3Digest},
		{"reference-labels", referenceLabelsInput, referenceLabelsV3Norm, referenceLabelsV3Digest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got := normalise(t, tt.input, v4alpha2.Algorithm)
			r.Equal(tt.wantNorm, got, "v4alpha2 must reproduce the legacy v3 bytes") //nolint:testifylint // byte-exact comparison, JSONEq would mask encoding differences
			sum := sha256.Sum256([]byte(got))
			r.Equal(tt.wantHex, hex.EncodeToString(sum[:]), "v4alpha2 digest must match the v3 oracle digest")
		})
	}
}

// TestSameAsV4alpha1WithoutVersionedLabels pins that v4alpha2 only diverges from
// the frozen v4alpha1 when a label carries a version. With no versioned labels
// present the two normalise identically (and match the v3 oracle), so repointing
// the v3 alias from v4alpha1 to v4alpha2 cannot break such existing signatures.
func TestSameAsV4alpha1WithoutVersionedLabels(t *testing.T) {
	r := require.New(t)
	v1out := normalise(t, deltaFreeInput, v4alpha1.Algorithm)
	v2out := normalise(t, deltaFreeInput, v4alpha2.Algorithm)
	r.Equal(v1out, v2out, "version-free input must normalise identically under v4alpha1 and v4alpha2")
	r.Equal(deltaFreeV3Norm, v2out, "and identically to legacy v3") //nolint:testifylint // byte-exact comparison, JSONEq would mask encoding differences
}

// TestVersionedLabelsDivergeFromV4alpha1 proves the fix: v4alpha2 signs
// label.version at every position (component, resource, source) where v4alpha1
// drops it, so the bytes diverge and the version appears only in v4alpha2.
func TestVersionedLabelsDivergeFromV4alpha1(t *testing.T) {
	r := require.New(t)
	v1out := normalise(t, versionLabelsInput, v4alpha1.Algorithm)
	v2out := normalise(t, versionLabelsInput, v4alpha2.Algorithm)

	r.NotEqual(v1out, v2out, "label version must change the signed bytes vs v4alpha1")
	r.NotContains(v1out, `"version":"v1"`, "v4alpha1 must not sign label.version")
	r.Contains(v2out, `"version":"v1"`, "v4alpha2 must sign component label.version")
	r.Contains(v2out, `"version":"v3"`, "v4alpha2 must sign resource label.version")
	r.Contains(v2out, `"version":"v4"`, "v4alpha2 must sign source label.version")
}

// TestReferenceLabelsUnfilteredWithVersion proves v4alpha2 keeps reference
// labels exactly as legacy v3 did: both the signing and the non-signing label
// survive, and the signing label's version is signed. This is where v4alpha1
// diverged (it stripped label.version) and where v4alpha3 will diverge the other
// way (it filters the non-signing label out).
func TestReferenceLabelsUnfilteredWithVersion(t *testing.T) {
	r := require.New(t)
	v1out := normalise(t, referenceLabelsInput, v4alpha1.Algorithm)
	v2out := normalise(t, referenceLabelsInput, v4alpha2.Algorithm)

	r.Equal(referenceLabelsV3Norm, v2out, "v4alpha2 reference labels must be byte-identical to legacy v3") //nolint:testifylint // byte-exact comparison, JSONEq would mask encoding differences
	r.Contains(v2out, `"name":"ref-sig"`, "v4alpha2 keeps the signing reference label")
	r.Contains(v2out, `"name":"ref-nosig"`, "v4alpha2 keeps the non-signing reference label unfiltered, like v3")
	r.Contains(v2out, `"version":"v5"`, "v4alpha2 signs the reference label version")
	r.NotEqual(v1out, v2out, "v4alpha1 drops the reference label version, so it diverges")
	r.NotContains(v1out, `"version":"v5"`, "v4alpha1 must not sign the reference label version")
}
