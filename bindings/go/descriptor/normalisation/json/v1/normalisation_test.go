package v1_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
		{"15-creationtime-offset-positive", input_15_creationtime_offset_positive, norm_15_creationtime_offset_positive, digest_15_creationtime_offset_positive},
		{"16-creationtime-offset-negative", input_16_creationtime_offset_negative, norm_16_creationtime_offset_negative, digest_16_creationtime_offset_negative},
		{"17-resource-type-aliases", input_17_resource_type_aliases, norm_17_resource_type_aliases, digest_17_resource_type_aliases},
		{"18-label-float", input_18_label_float, norm_18_label_float, digest_18_label_float},
		{"19-label-numbers", input_19_label_numbers, norm_19_label_numbers, digest_19_label_numbers},
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

// TestNumberDivergenceOneFloat documents the one numeric case OCM v2 cannot
// reproduce against cc-utils: an integral float label value. cc-utils signs it
// as "1.0"; OCM v2's store read collapses 1.0 -> 1 before normalisation, so
// this engine emits "1". The test locks the actual (divergent) output and
// asserts it is NOT the cc-utils form, so the gap stays visible and any future
// change that accidentally "fixes" or worsens it is caught.
func TestNumberDivergenceOneFloat(t *testing.T) {
	r := require.New(t)
	desc := fromYAML(t, input_20_label_one_float)

	got, err := normalisation.Normalise(desc, v1.Algorithm)
	r.NoError(err)
	// Byte-exact, NOT JSONEq: the whole point is the spelling 1 vs 1.0, which
	// JSONEq treats as equal.
	r.Equal(norm_20_ocmv2, string(got))      //nolint:testifylint // byte-exact spelling check, JSONEq would mask 1 vs 1.0
	r.NotEqual(norm_20_ccutils, string(got)) //nolint:testifylint // byte-exact spelling check, JSONEq would mask 1 vs 1.0
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

func TestOCMv1Applicable(t *testing.T) {
	const descYAML = `
meta:
  schemaVersion: v2
component:
  name: acme.org/numbers
  version: 1.0.0
  provider: '{"name":"acme","labels":[{"name":"p","value":%s}]}'
  repositoryContexts: []
  componentReferences: []
  sources: []
  resources:
  - name: res
    version: 1.0.0
    type: blob
    relation: local
    extraIdentity:
      arch: amd64
    access:
      type: localBlob/v1
      localReference: sha256:abc
      mediaType: application/octet-stream
    labels:
    - {name: r, value: {nested: [%s]}, signing: true}
  labels:
  - {name: c, value: %s}
`
	descriptor := func(t *testing.T, provider, resource, component string) *descruntime.Descriptor {
		t.Helper()
		var v2desc v2.Descriptor
		require.NoError(t, yaml.Unmarshal([]byte(fmt.Sprintf(descYAML, provider, resource, component)), &v2desc))
		desc, err := descruntime.ConvertFromV2(&v2desc)
		require.NoError(t, err)
		return desc
	}
	const ok, bad = "9007199254740992", "9007199254740993"

	require.NoError(t, v1.OCMv1Applicable(descriptor(t, ok, "0.00001", "1.5")))
	for name, d := range map[string][3]string{
		"provider label":  {bad, ok, ok},
		"resource label":  {ok, bad, ok},
		"component label": {ok, ok, bad},
	} {
		t.Run(name, func(t *testing.T) {
			err := v1.OCMv1Applicable(descriptor(t, d[0], d[1], d[2]))
			require.ErrorIs(t, err, v1.ErrInexactNumber)
			require.ErrorContains(t, err, bad)
		})
	}
}

func TestOCMv1ApplicableObjectsInArrays(t *testing.T) {
	const descYAML = `
meta:
  schemaVersion: v2
component:
  name: acme.org/arrays
  version: 1.0.0
  provider: '{"name":"acme","labels":[{"name":"p","value":%s,"signing":true}]}'
  repositoryContexts:
  - {type: OCIRegistry, baseUrl: example.com}
  componentReferences:
  - name: ref
    componentName: acme.org/ref
    version: 1.0.0
    labels:
    - {name: x, value: 1, signing: true}
  sources:
  - name: src
    version: 1.0.0
    type: git
    access: {type: none}
    labels:
    - {name: s, value: %s, signing: true}
  resources:
  - name: res
    version: 1.0.0
    type: blob
    relation: local
    access: {type: localBlob/v1, localReference: sha256:abc, mediaType: application/octet-stream, extra: %s}
    srcRefs:
    - identitySelector: {name: src}
    labels:
    - {name: r, value: %s, signing: true}
  labels:
  - {name: c, value: %s, signing: true}
`
	const plain = `[1, "x", null, [2]]`
	const objInArray = "[{b: 1, a: 2}]"
	for _, tc := range []struct {
		name                                string
		provider, source, access, res, comp string
		ambiguous                           bool
	}{
		{name: "element lists and arrays of scalars", provider: plain, source: plain, access: plain, res: plain, comp: plain},
		{name: "sources are not normalised", provider: plain, source: objInArray, access: plain, res: plain, comp: plain},
		{name: "component label value", provider: plain, source: plain, access: plain, res: plain, comp: objInArray, ambiguous: true},
		{name: "resource label value nested", provider: plain, source: plain, access: plain, res: "{x: [[{a: 1}]]}", comp: plain, ambiguous: true},
		{name: "resource access and srcRefs are not normalised", provider: plain, source: plain, access: objInArray, res: plain, comp: plain},
		{name: "provider label value", provider: `[{"a":1}]`, source: plain, access: plain, res: plain, comp: plain, ambiguous: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			var v2desc v2.Descriptor
			r.NoError(yaml.Unmarshal([]byte(fmt.Sprintf(descYAML, tc.provider, tc.source, tc.access, tc.res, tc.comp)), &v2desc))
			desc, err := descruntime.ConvertFromV2(&v2desc)
			r.NoError(err)
			err = v1.OCMv1Applicable(desc)
			if tc.ambiguous {
				r.ErrorIs(err, v1.ErrAmbiguousArrayObjects)
				return
			}
			r.NoError(err)
		})
	}
}

// TestNormaliseOCMv1ObjectsInArrays pins NormaliseOCMv1 to OCM v1, whose entry
// engine also rewrites objects inside arrays. The expected bytes are the output
// of OCM v1 0.52.0's compdesc.Normalize(cd, "jsonNormalisation/v1").
func TestNormaliseOCMv1ObjectsInArrays(t *testing.T) {
	const labelYAML = `
meta:
  schemaVersion: v2
component:
  name: acme.org/numbers
  version: 1.0.0
  provider: acme
  repositoryContexts: []
  componentReferences: []
  sources: []
  resources: []
  labels:
  - {name: num, value: %s, signing: true}
`
	const labelNorm = `[{"component":[{"componentReferences":[]},{"labels":[[{"name":"num"},{"signing":true},{"value":%s}]]},{"name":"acme.org/numbers"},{"provider":"acme"},{"resources":[]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`
	const elementsYAML = `
meta:
  schemaVersion: v2
component:
  name: acme.org/elements
  version: 1.0.0
  provider: '{"name":"acme","labels":[{"name":"p","value":[{"y":1,"x":2}],"signing":true}]}'
  repositoryContexts: []
  sources: []
  componentReferences:
  - name: ref
    componentName: acme.org/child
    version: 1.0.0
    labels:
    - {name: rl, value: [[{b: 1}], {a: [2]}], signing: true}
  resources:
  - name: res
    version: 1.0.0
    type: plainText
    relation: local
    access:
      type: localBlob/v1
      localReference: sha256:abc
      mediaType: text/plain
    labels:
    - {name: l, value: [{k: v, j: [{m: 1, l: 0}]}], signing: true}
`
	const elementsNorm = `[{"component":[{"componentReferences":[[{"componentName":"acme.org/child"},{"extraIdentity":null},{"labels":[[{"name":"rl"},{"signing":true},{"value":[[[{"b":1}]],[{"a":[2]}]]}]]},{"name":"ref"},{"version":"1.0.0"}]]},{"name":"acme.org/elements"},{"provider":[{"labels":[[{"name":"p"},{"signing":true},{"value":[[{"x":2},{"y":1}]]}]]},{"name":"acme"}]},{"resources":[[{"extraIdentity":null},{"labels":[[{"name":"l"},{"signing":true},{"value":[[{"j":[[{"l":0},{"m":1}]]},{"k":"v"}]]}]]},{"name":"res"},{"relation":"local"},{"type":"plainText"},{"version":"1.0.0"}]]},{"version":"1.0.0"}]},{"meta":[{"schemaVersion":"v2"}]}]`

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "list of maps",
			input: fmt.Sprintf(labelYAML, `[{b: 1, a: 2}]`),
			want:  fmt.Sprintf(labelNorm, `[[{"a":2},{"b":1}]]`),
		},
		{
			name:  "arrays of arrays keep order, scalars and nulls",
			input: fmt.Sprintf(labelYAML, `[[{b: 1, a: 2}], [], 3, null, "s"]`),
			want:  fmt.Sprintf(labelNorm, `[[[{"a":2},{"b":1}]],[],3,null,"s"]`),
		},
		{
			name:  "maps in arrays in maps",
			input: fmt.Sprintf(labelYAML, `{x: [{d: {f: 1, e: [{h: 2, g: 3}]}, c: 4}]}`),
			want:  fmt.Sprintf(labelNorm, `[{"x":[[{"c":4},{"d":[{"e":[[{"g":3},{"h":2}]]},{"f":1}]}]]}]`),
		},
		{
			name:  "empty map and null value in array",
			input: fmt.Sprintf(labelYAML, `[{}, {a: null}]`),
			want:  fmt.Sprintf(labelNorm, `[[],[{"a":null}]]`),
		},
		{
			name:  "provider, resource and reference labels",
			input: elementsYAML,
			want:  elementsNorm,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got, err := v1.NormaliseOCMv1(fromYAML(t, tt.input))
			r.NoError(err)
			r.Equal(tt.want, string(got))
		})
	}
}
