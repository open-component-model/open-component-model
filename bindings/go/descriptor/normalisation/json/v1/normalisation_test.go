package v1_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/signing"
)

// fromJSON loads an input cd.json const into the runtime descriptor, mirroring
// the signing flow (v2.Descriptor → ConvertFromV2).
func fromJSON(t *testing.T, in string) *descruntime.Descriptor {
	t.Helper()
	r := require.New(t)
	var desc v2.Descriptor
	r.NoError(json.Unmarshal([]byte(in), &desc))
	rt, err := descruntime.ConvertFromV2(&desc)
	r.NoError(err)
	return rt
}

// TestNormaliseByteExact asserts the normalised output is byte-identical to the
// cc-utils known-answer bytes and that sha256(output) matches the oracle digest.
func TestNormaliseByteExact(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		digest   string
	}{
		{"01-minimal-oci", input_01_minimal_oci, norm_01_minimal_oci, digest_01_minimal_oci},
		{"02-localblob-generic", input_02_localblob_generic, norm_02_localblob_generic, digest_02_localblob_generic},
		{"03-exclude-from-signature", input_03_exclude_from_signature, norm_03_exclude_from_signature, digest_03_exclude_from_signature},
		{"04-labels", input_04_labels, norm_04_labels, digest_04_labels},
		{"05-duplicate-identity", input_05_duplicate_identity, norm_05_duplicate_identity, digest_05_duplicate_identity},
		{"06-creationtime-rounding", input_06_creationtime_rounding, norm_06_creationtime_rounding, digest_06_creationtime_rounding},
		{"07-child", input_07_child, norm_07_child, digest_07_child},
		{"07-root-with-reference", input_07_root_with_reference, norm_07_root_with_reference, digest_07_root_with_reference},
		{"08-full-signed", input_08_full_signed, norm_08_full_signed, digest_08_full_signed},
		{"09-creationtime-offset-fraction", input_09_creationtime_offset_fraction, norm_09_creationtime_offset_fraction, digest_09_creationtime_offset_fraction},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			desc := fromJSON(t, tt.input)

			got, err := normalisation.Normalise(desc, v1.Algorithm)
			r.NoError(err)
			r.Equal(tt.expected, string(got), "normalised bytes diverge from cc-utils oracle")

			sum := sha256.Sum256(got)
			r.Equal(tt.digest, hex.EncodeToString(sum[:]), "digest diverges from cc-utils oracle")
		})
	}
}

// TestVerifyDigestMatchesDescriptor exercises the real verify entrypoint on the
// combined scenario 08 descriptor. It does not check a cryptographic signature.
func TestVerifyDigestMatchesDescriptor(t *testing.T) {
	r := require.New(t)
	desc := fromJSON(t, input_08_full_signed)
	sig := descruntime.Signature{
		Name: "acme",
		Digest: descruntime.Digest{
			HashAlgorithm:          "SHA-256",
			NormalisationAlgorithm: v1.Algorithm,
			Value:                  digest_08_full_signed,
		},
	}
	r.NoError(signing.VerifyDigestMatchesDescriptor(t.Context(), desc, sig, slog.Default()))
}

// TestGenerateDigestRoundTrip confirms creation reproduces the oracle digest via
// the algorithm-agnostic signing registry.
func TestGenerateDigestRoundTrip(t *testing.T) {
	r := require.New(t)
	desc := fromJSON(t, input_08_full_signed)
	d, err := signing.GenerateDigest(t.Context(), desc, slog.Default(), v1.Algorithm, "SHA-256")
	r.NoError(err)
	r.Equal(digest_08_full_signed, d.Value)
	r.Equal(v1.Algorithm, d.NormalisationAlgorithm)
}

// TestSentinelAccepted confirms a descriptor carrying an EXCLUDE-FROM-SIGNATURE
// resource is not rejected by the digest completeness/algorithm checks.
func TestSentinelAccepted(t *testing.T) {
	r := require.New(t)
	desc := fromJSON(t, input_03_exclude_from_signature)
	r.NoError(signing.IsSafelyDigestible(&desc.Component))
	r.NoError(signing.ValidateDigestHashAlgorithms(&desc.Component))
}

// TestUnknownAlgorithmErrors locks in that an unregistered normalisation
// algorithm still errors at the registry, so registering v1 did not weaken the
// unknown-algorithm guard.
func TestUnknownAlgorithmErrors(t *testing.T) {
	r := require.New(t)
	desc := fromJSON(t, input_01_minimal_oci)
	_, err := normalisation.Normalise(desc, "jsonNormalisation/bogus")
	r.ErrorIs(err, normalisation.ErrUnknownNormalisationAlgorithm)
}
