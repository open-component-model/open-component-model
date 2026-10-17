package v1_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

// fromYAML loads an input cd const into the runtime descriptor, mirroring the
// signing flow (v2.Descriptor → ConvertFromV2).
func fromYAML(t *testing.T, in string) *descruntime.Descriptor {
	t.Helper()
	r := require.New(t)
	var desc v2.Descriptor
	r.NoError(yaml.Unmarshal([]byte(in), &desc))
	rt, err := descruntime.ConvertFromV2(&desc)
	r.NoError(err)
	return rt
}

// TestNormaliseByteExact asserts the normalised output is byte-identical to the
// cc-utils reference bytes and that sha256(output) matches cc-utils' digest.
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
		{"10-unicode-label", input_10_unicode_label, norm_10_unicode_label, digest_10_unicode_label},
		{"11-dup-delimiter", input_11_dup_delimiter, norm_11_dup_delimiter, digest_11_dup_delimiter},
		{"12-dup-explicit-version", input_12_dup_explicit_version, norm_12_dup_explicit_version, digest_12_dup_explicit_version},
		{"14-label-list-value", input_14_label_list_value, norm_14_label_list_value, digest_14_label_list_value},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			desc := fromYAML(t, tt.input)

			got, err := normalisation.Normalise(desc, v1.Algorithm)
			r.NoError(err)
			r.Equal(tt.expected, string(got), "normalised bytes diverge from cc-utils")

			sum := sha256.Sum256(got)
			r.Equal(tt.digest, hex.EncodeToString(sum[:]), "digest diverges from cc-utils")
		})
	}
}

// TestUnknownAlgorithmErrors locks in that an unregistered normalisation
// algorithm still errors at the registry, so registering v1 did not weaken the
// unknown-algorithm guard.
func TestUnknownAlgorithmErrors(t *testing.T) {
	r := require.New(t)
	desc := fromYAML(t, input_01_minimal_oci)
	_, err := normalisation.Normalise(desc, "jsonNormalisation/bogus")
	r.ErrorIs(err, normalisation.ErrUnknownNormalisationAlgorithm)
}
