package algorithms_test

import (
	"bytes"
	"crypto"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	jsonnormv1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
)

const (
	jsonV1   = "jsonNormalisation/v1"
	v4alpha1 = "jsonNormalisation/v4alpha1"
	v4alpha2 = "jsonNormalisation/v4alpha2"
	v4alpha3 = "jsonNormalisation/v4alpha3"
)

// testDescriptor carries a versioned signing label, so v4alpha1 (drops
// label.version) and v4alpha2 (keeps it) produce different bytes.
func testDescriptor() *descruntime.Descriptor {
	return &descruntime.Descriptor{
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{
				ObjectMeta: descruntime.ObjectMeta{
					Name:    "ocm.software/test",
					Version: "1.0.0",
					Labels:  []descruntime.Label{{Name: "sig", Value: []byte(`"v"`), Signing: true, Version: "v1"}},
				},
			},
			Provider: descruntime.Provider{Name: "ocm.software"},
		},
	}
}

// TestRegistersVerifiableFamily asserts that importing algorithms alone (this
// file imports no json/* package) registers every algorithm that must stay
// verifiable.
func TestRegistersVerifiableFamily(t *testing.T) {
	r := require.New(t)
	for _, algo := range []string{jsonV1, v4alpha1, v4alpha2, v4alpha3} {
		r.NotNil(normalisation.Normalisations.Get(algo), "%s must be registered", algo)
	}
}

func TestDefault(t *testing.T) {
	require.Equal(t, v4alpha3, algorithms.Default)
}

func TestResolve(t *testing.T) {
	tests := []struct {
		in       string
		want     string
		resolved bool
	}{
		{in: algorithms.LegacyV3, want: v4alpha2, resolved: true},
		{in: v4alpha1, want: v4alpha1},
		{in: v4alpha3, want: v4alpha3},
		{in: "unknown", want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			r := require.New(t)
			got, resolved := algorithms.Resolve(tt.in)
			r.Equal(tt.want, got)
			r.Equal(tt.resolved, resolved)
		})
	}
}

func TestHash(t *testing.T) {
	tests := []struct {
		name    string
		want    crypto.Hash
		wantErr bool
	}{
		{name: "SHA-256", want: crypto.SHA256},
		{name: "sha-256", want: crypto.SHA256},
		{name: "SHA256", want: crypto.SHA256},
		{name: "sha256", want: crypto.SHA256},
		{name: "SHA-512", want: crypto.SHA512},
		{name: "sha512", want: crypto.SHA512},
		{name: "sha-512", want: crypto.SHA512},
		{name: "S-H-A-2-5-6", wantErr: true},
		{name: "--sha512--", wantErr: true},
		{name: "sha_256", wantErr: true},
		{name: "SHA-256 ", wantErr: true},
		{name: "MD5", wantErr: true},
		{name: "SHA-1", wantErr: true},
		{name: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			got, err := algorithms.Hash(tt.name)
			if tt.wantErr {
				r.ErrorIs(err, algorithms.ErrUnsupportedHash)
				r.ErrorContains(err, "unsupported hash algorithm")
				return
			}
			r.NoError(err)
			r.Equal(tt.want, got)
		})
	}
}

func TestDigest(t *testing.T) {
	r := require.New(t)
	desc := testDescriptor()

	normalised, err := normalisation.Normalise(desc, v4alpha3)
	r.NoError(err)

	got, err := algorithms.Digest(desc, v4alpha3, "sha256")
	r.NoError(err)
	r.Equal(&descruntime.Digest{
		HashAlgorithm:          crypto.SHA256.String(),
		NormalisationAlgorithm: v4alpha3,
		Value:                  digest.SHA256.FromBytes(normalised).Encoded(),
	}, got, "hash name is written canonically")

	legacy, err := algorithms.Digest(desc, algorithms.LegacyV3, crypto.SHA256.String())
	r.NoError(err)
	a2, err := algorithms.Digest(desc, v4alpha2, crypto.SHA256.String())
	r.NoError(err)
	r.Equal(a2, legacy, "the legacy v3 alias is computed and labelled as v4alpha2")

	_, err = algorithms.Digest(desc, "unknown", crypto.SHA256.String())
	r.ErrorIs(err, normalisation.ErrUnknownNormalisationAlgorithm)

	_, err = algorithms.Digest(desc, v4alpha3, "MD5")
	r.ErrorIs(err, algorithms.ErrUnsupportedHash)
}

func TestVerify(t *testing.T) {
	desc := testDescriptor()
	mustDigest := func(t *testing.T, norm, hash string) descruntime.Digest {
		t.Helper()
		d, err := algorithms.Digest(desc, norm, hash)
		require.NoError(t, err)
		return *d
	}
	a1 := mustDigest(t, v4alpha1, crypto.SHA256.String())
	a2 := mustDigest(t, v4alpha2, crypto.SHA256.String())
	a3sha512 := mustDigest(t, v4alpha3, crypto.SHA512.String())
	require.NotEqual(t, a1.Value, a2.Value, "v4alpha1 and v4alpha2 must differ for this descriptor or the alias case proves nothing")

	tests := []struct {
		name    string
		digest  descruntime.Digest
		wantErr error
	}{
		{name: "match", digest: a1},
		{name: "sha512", digest: a3sha512},
		{name: "legacy hash name", digest: descruntime.Digest{HashAlgorithm: "sha256", NormalisationAlgorithm: a1.NormalisationAlgorithm, Value: a1.Value}},
		{name: "upper-case hex", digest: descruntime.Digest{HashAlgorithm: a1.HashAlgorithm, NormalisationAlgorithm: a1.NormalisationAlgorithm, Value: strings.ToUpper(a1.Value)}},
		{name: "legacy v3 alias", digest: descruntime.Digest{HashAlgorithm: a2.HashAlgorithm, NormalisationAlgorithm: algorithms.LegacyV3, Value: a2.Value}},
		{name: "mismatch", digest: descruntime.Digest{HashAlgorithm: a1.HashAlgorithm, NormalisationAlgorithm: a1.NormalisationAlgorithm, Value: a2.Value}, wantErr: algorithms.ErrDigestMismatch},
		{name: "unknown normalisation", digest: descruntime.Digest{HashAlgorithm: a1.HashAlgorithm, NormalisationAlgorithm: "unknown", Value: a1.Value}, wantErr: normalisation.ErrUnknownNormalisationAlgorithm},
		{name: "legacy v2 is rejected", digest: descruntime.Digest{HashAlgorithm: a1.HashAlgorithm, NormalisationAlgorithm: algorithms.LegacyV2, Value: a1.Value}, wantErr: algorithms.ErrUnsupportedNormalisation},
		{name: "unsupported hash", digest: descruntime.Digest{HashAlgorithm: "MD5", NormalisationAlgorithm: a1.NormalisationAlgorithm, Value: a1.Value}, wantErr: algorithms.ErrUnsupportedHash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := require.New(t)
			err := algorithms.Verify(desc, tt.digest)
			if tt.wantErr != nil {
				r.ErrorIs(err, tt.wantErr)
				return
			}
			r.NoError(err)
		})
	}

	t.Run("non-hex value", func(t *testing.T) {
		r := require.New(t)
		err := algorithms.Verify(desc, descruntime.Digest{HashAlgorithm: a1.HashAlgorithm, NormalisationAlgorithm: a1.NormalisationAlgorithm, Value: "not-hex"})
		r.ErrorIs(err, algorithms.ErrDigestMismatch)
	})
}

func TestLogNonDefaultReferenceDigest(t *testing.T) {
	warnFor := func(d descruntime.Digest) string {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		algorithms.LogNonDefaultReferenceDigest(t.Context(), logger, slog.LevelWarn, d, "reference", "child")
		return buf.String()
	}
	r := require.New(t)

	r.Empty(warnFor(descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: v4alpha3, Value: "aa"}), "the default must not warn")

	out := warnFor(descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: v4alpha1, Value: "bb"})
	r.Contains(out, "level=WARN")
	r.Contains(out, "reference=child", "caller attributes identify the reference")
	r.Contains(out, "normalisationAlgorithm="+v4alpha1)
	r.Contains(out, "recommended="+v4alpha3)
	r.Contains(out, "component reference digest was computed with an older normalisation algorithm")
	r.Contains(out, "migration="+algorithms.MigrationGuide)

	missing := warnFor(descruntime.Digest{})
	r.Contains(missing, "component reference has no digest", "a missing digest gets its own warning")
	r.Contains(missing, "reference=child")
	r.Contains(missing, "normalisationAlgorithm="+algorithms.NoReferenceDigest)
	r.Contains(warnFor(descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: algorithms.LegacyV3, Value: "cc"}), "normalisationAlgorithm="+algorithms.LegacyV3, "the legacy alias is reported as written")
}

func TestVerifyOnly(t *testing.T) {
	r := require.New(t)
	r.True(algorithms.VerifyOnly(jsonV1))
	for _, algo := range []string{v4alpha1, v4alpha2, algorithms.LegacyV3, v4alpha3} {
		r.False(algorithms.VerifyOnly(algo), algo)
	}
}

func TestLogNonDefaultDigest(t *testing.T) {
	warnFor := func(algo string) string {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		algorithms.LogNonDefaultDigest(t.Context(), logger, slog.LevelWarn, algo, "component", "c")
		return buf.String()
	}
	r := require.New(t)

	r.Empty(warnFor(v4alpha3), "the default must not warn")
	for _, algo := range []string{jsonV1, algorithms.LegacyV3, v4alpha1, v4alpha2} {
		out := warnFor(algo)
		r.Contains(out, "level=WARN", algo)
		r.Contains(out, "trusted digest was computed with an older normalisation algorithm", algo)
		r.Contains(out, "migration="+algorithms.MigrationGuide, algo)
		r.Contains(out, "normalisationAlgorithm="+algo, "%s is reported as written", algo)
		r.Contains(out, "recommended="+v4alpha3, algo)
		r.Contains(out, "component=c", "caller attributes identify what is trusted")
	}
}

// Callers that cannot act on the message (the controller, references of
// component versions published by others) log it below warning level.
func TestLogNonDefaultHonoursLevel(t *testing.T) {
	r := require.New(t)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}))

	algorithms.LogNonDefaultDigest(t.Context(), logger, slog.LevelDebug, v4alpha1)
	algorithms.LogNonDefaultReferenceDigest(t.Context(), logger, slog.LevelDebug, descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: v4alpha1, Value: "bb"})
	algorithms.LogNonDefaultReferenceDigest(t.Context(), logger, slog.LevelDebug, descruntime.Digest{})
	r.Empty(buf.String(), "debug messages must not reach an info logger")

	algorithms.LogNonDefaultDigest(t.Context(), logger, slog.LevelInfo, v4alpha1)
	r.Contains(buf.String(), "level=INFO")
}

// nullLabelDescriptor carries a signed label with value: null on the
// component and on a component reference, as OCM v1 signs them.
func nullLabelDescriptor() *descruntime.Descriptor {
	null := []descruntime.Label{{Name: "foo", Value: []byte(`null`), Signing: true}}
	return &descruntime.Descriptor{
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: "ocm.software/null", Version: "1.0.0", Labels: null},
			},
			Provider: descruntime.Provider{Name: "ocm.software"},
			References: []descruntime.Reference{{
				ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "ref", Version: "1.0.0", Labels: null}},
				Component:   "ocm.software/ref",
			}},
		},
	}
}

// legacyV3NullBytes is what OCM v1 jsonNormalisation/v3 signs for
// nullLabelDescriptor: "value":null is kept on both labels.
const legacyV3NullBytes = `{"component":{"componentReferences":[{"componentName":"ocm.software/ref","labels":[{"name":"foo","signing":true,"value":null}],"name":"ref","version":"1.0.0"}],` +
	`"labels":[{"name":"foo","signing":true,"value":null}],"name":"ocm.software/null","provider":{"name":"ocm.software"},"resources":[],"sources":[],"version":"1.0.0"}}`

// TestVerifyNullLabelValue: a jsonNormalisation/v3 digest is verified over the
// v1 bytes only (null label values kept), while a digest v2 recorded as v4alpha2
// over the same descriptor keeps verifying unchanged, and minting with the v3
// alias still records v4alpha2.
func TestVerifyNullLabelValue(t *testing.T) {
	desc := nullLabelDescriptor()
	sha := func(s string) string { return digest.SHA256.FromString(s).Encoded() }
	v3 := func(value string) descruntime.Digest {
		return descruntime.Digest{HashAlgorithm: crypto.SHA256.String(), NormalisationAlgorithm: algorithms.LegacyV3, Value: value}
	}

	recorded, err := algorithms.Digest(desc, algorithms.LegacyV3, crypto.SHA256.String())
	require.NoError(t, err)
	require.Equal(t, v4alpha2, recorded.NormalisationAlgorithm, "minting with the v3 alias must still record v4alpha2")
	frozen, err := normalisation.Normalise(desc, v4alpha2)
	require.NoError(t, err)
	require.NotContains(t, string(frozen), `"value":null`, "v4alpha2 must stay byte-frozen")
	require.Equal(t, sha(string(frozen)), recorded.Value)

	tests := []struct {
		name    string
		digest  descruntime.Digest
		wantErr error
	}{
		{name: "v1 v3 signature over null label values", digest: v3(sha(legacyV3NullBytes))},
		{name: "recorded v4alpha2 digest", digest: *recorded},
		{name: "v3 digest over the v4alpha2 bytes", digest: v3(recorded.Value), wantErr: algorithms.ErrDigestMismatch},
		{name: "v4alpha2 digest over the v3 bytes", digest: descruntime.Digest{HashAlgorithm: crypto.SHA256.String(), NormalisationAlgorithm: v4alpha2, Value: sha(legacyV3NullBytes)}, wantErr: algorithms.ErrDigestMismatch},
		{name: "v3 mismatch", digest: v3(sha(legacyV3NullBytes + " ")), wantErr: algorithms.ErrDigestMismatch},
		{name: "v3 unsupported hash", digest: descruntime.Digest{HashAlgorithm: "MD5", NormalisationAlgorithm: algorithms.LegacyV3, Value: sha(legacyV3NullBytes)}, wantErr: algorithms.ErrUnsupportedHash},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := algorithms.Verify(desc, tt.digest)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestLegacyV2IsRejectedWithResignHint(t *testing.T) {
	desc := &descruntime.Descriptor{}
	_, err := algorithms.Digest(desc, algorithms.LegacyV2, crypto.SHA256.String())
	require.ErrorIs(t, err, algorithms.ErrUnsupportedNormalisation)
	require.ErrorIs(t, err, normalisation.ErrUnknownNormalisationAlgorithm)
	require.ErrorContains(t, err, "jsonNormalisation/v2")
	require.ErrorContains(t, err, "re-sign")
}

// ocmV1VariantYAML exercises every spot where the OCM v1 CLI's
// jsonNormalisation/v1 differs from cc-utils: a resource type cc-utils
// canonicalises, provider labels, and signed label values with HTML
// characters, an exponent float and the integer 2^53 (exact in float64).
const ocmV1VariantYAML = `
meta:
  schemaVersion: v2
component:
  name: acme.org/compat
  version: 1.0.0
  provider: '{"name":"ACME","labels":[{"name":"p","value":"x","signing":true},{"name":"unsigned","value":"y"}]}'
  repositoryContexts: []
  componentReferences: []
  sources: []
  resources:
  - name: res
    version: 1.0.0
    type: blob
    relation: local
    access:
      type: ociArtifact/v1
      imageReference: registry.example/img:1
    digest:
      hashAlgorithm: SHA-256
      normalisationAlgorithm: genericBlobDigest/v1
      value: cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
    labels:
    - {name: html, value: "a<b>&c", signing: true}
    - {name: sep, value: "line\u2028sep", signing: true}
    - {name: small, value: 0.00001, signing: true}
    - {name: big, value: 9007199254740992, signing: true}
`

func TestVerifyJSONNormalisationV1Variants(t *testing.T) {
	var v2desc v2.Descriptor
	require.NoError(t, yaml.Unmarshal([]byte(ocmV1VariantYAML), &v2desc))
	desc, err := descruntime.ConvertFromV2(&v2desc)
	require.NoError(t, err)

	digestOf := func(t *testing.T, normalise func(*descruntime.Descriptor) ([]byte, error)) descruntime.Digest {
		t.Helper()
		b, err := normalise(desc)
		require.NoError(t, err)
		sum := sha256.Sum256(b)
		return descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: jsonV1, Value: hex.EncodeToString(sum[:])}
	}
	ccutils := digestOf(t, func(d *descruntime.Descriptor) ([]byte, error) { return normalisation.Normalise(d, jsonV1) })
	ocmv1 := digestOf(t, jsonnormv1.NormaliseOCMv1)

	t.Run("variants differ", func(t *testing.T) {
		require.NotEqual(t, ccutils.Value, ocmv1.Value)
	})
	t.Run("OCM v1 bytes", func(t *testing.T) {
		b, err := jsonnormv1.NormaliseOCMv1(desc)
		require.NoError(t, err)
		s := string(b)
		require.Contains(t, s, `"a\u003cb\u003e\u0026c"`)
		require.Contains(t, s, `"line\u2028sep"`)
		require.Contains(t, s, `0.00001`)
		require.Contains(t, s, `9007199254740992`)
		require.Contains(t, s, `{"type":"blob"}`)
		require.Contains(t, s, `{"name":"p"}`)
		require.NotContains(t, s, "unsigned")
	})
	t.Run("cc-utils digest verifies", func(t *testing.T) {
		require.NoError(t, algorithms.Verify(desc, ccutils))
	})
	t.Run("OCM v1 digest verifies", func(t *testing.T) {
		require.NoError(t, algorithms.Verify(desc, ocmv1))
	})
	t.Run("other digest fails with actionable error", func(t *testing.T) {
		bad := ccutils
		bad.Value = strings.Repeat("0", 64)
		err := algorithms.Verify(desc, bad)
		require.ErrorIs(t, err, algorithms.ErrDigestMismatch)
		require.ErrorContains(t, err, "OCM v1 variant")
		require.ErrorContains(t, err, "re-sign")
	})
}

// numberYAML is a descriptor on which the cc-utils and OCM v1 variants differ
// only in number spelling: a bare provider, no canonicalised resource type and
// a single signed label whose value is substituted per case.
const numberYAML = `
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

func numberDescriptor(t *testing.T, value string) *descruntime.Descriptor {
	t.Helper()
	var v2desc v2.Descriptor
	require.NoError(t, yaml.Unmarshal([]byte(strings.Replace(numberYAML, "%s", value, 1)), &v2desc))
	desc, err := descruntime.ConvertFromV2(&v2desc)
	require.NoError(t, err)
	return desc
}

func jsonV1DigestOf(t *testing.T, desc *descruntime.Descriptor, normalise func(*descruntime.Descriptor) ([]byte, error)) descruntime.Digest {
	t.Helper()
	b, err := normalise(desc)
	require.NoError(t, err)
	sum := sha256.Sum256(b)
	return descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: jsonV1, Value: hex.EncodeToString(sum[:])}
}

func ccUtilsNormalise(d *descruntime.Descriptor) ([]byte, error) {
	return normalisation.Normalise(d, jsonV1)
}

// TestVerifyJSONNormalisationV1FallbackKeepsCCUtilsExact guards the OCM v1
// fallback: two numbers with the same float64 value share one OCM v1 form, so
// the fallback must not let a changed number pass a cc-utils signature.
func TestVerifyJSONNormalisationV1FallbackKeepsCCUtilsExact(t *testing.T) {
	sameOCMv1Form := func(t *testing.T, a, b string) {
		t.Helper()
		x, err := jsonnormv1.NormaliseOCMv1(numberDescriptor(t, a))
		require.NoError(t, err)
		y, err := jsonnormv1.NormaliseOCMv1(numberDescriptor(t, b))
		require.NoError(t, err)
		require.Equal(t, string(x), string(y))
	}

	t.Run("integer above 2^53", func(t *testing.T) {
		const signed, tampered = "9007199254740992", "9007199254740993"
		sameOCMv1Form(t, signed, tampered)
		digest := jsonV1DigestOf(t, numberDescriptor(t, signed), ccUtilsNormalise)
		require.NoError(t, algorithms.Verify(numberDescriptor(t, signed), digest))
		require.ErrorIs(t, algorithms.Verify(numberDescriptor(t, tampered), digest), algorithms.ErrDigestMismatch)
	})

	// cc-utils itself reads fractions as Python floats, so 0.1 and
	// 0.10000000000000001 already share one cc-utils form and verify against
	// each other without the fallback (unchanged from before the fallback).
	// The fallback must not change that outcome. (The gate's token check for
	// 0.10000000000000001 is covered in json/v1.)
	t.Run("fraction", func(t *testing.T) {
		const signed, tampered = "0.1", "0.10000000000000001"
		sameOCMv1Form(t, signed, tampered)
		a, err := ccUtilsNormalise(numberDescriptor(t, signed))
		require.NoError(t, err)
		b, err := ccUtilsNormalise(numberDescriptor(t, tampered))
		require.NoError(t, err)
		require.Equal(t, string(a), string(b))

		digest := jsonV1DigestOf(t, numberDescriptor(t, signed), ccUtilsNormalise)
		require.NoError(t, algorithms.Verify(numberDescriptor(t, tampered), digest))
	})
}

func TestVerifyJSONNormalisationV1OCMv1NumberGate(t *testing.T) {
	t.Run("exact numbers verify via the OCM v1 variant", func(t *testing.T) {
		for _, value := range []string{"0.00001", "9007199254740992", "1.5"} {
			t.Run(value, func(t *testing.T) {
				desc := numberDescriptor(t, value)
				require.NoError(t, jsonnormv1.OCMv1Applicable(desc))
				ocmv1 := jsonV1DigestOf(t, desc, jsonnormv1.NormaliseOCMv1)
				require.NoError(t, algorithms.Verify(desc, ocmv1))
			})
		}
	})
	t.Run("0.00001 is spelled differently by the two variants", func(t *testing.T) {
		desc := numberDescriptor(t, "0.00001")
		require.NotEqual(t, jsonV1DigestOf(t, desc, ccUtilsNormalise).Value, jsonV1DigestOf(t, desc, jsonnormv1.NormaliseOCMv1).Value)
	})
	t.Run("inexact number fails with re-sign error", func(t *testing.T) {
		desc := numberDescriptor(t, "9007199254740993")
		require.ErrorIs(t, jsonnormv1.OCMv1Applicable(desc), jsonnormv1.ErrInexactNumber)
		ocmv1 := jsonV1DigestOf(t, desc, jsonnormv1.NormaliseOCMv1)
		err := algorithms.Verify(desc, ocmv1)
		require.ErrorIs(t, err, algorithms.ErrDigestMismatch)
		require.ErrorIs(t, err, jsonnormv1.ErrInexactNumber)
		require.ErrorContains(t, err, "OCM v1 variant")
		require.ErrorContains(t, err, "cannot represent exactly")
		require.ErrorContains(t, err, "re-sign")
		require.ErrorContains(t, err, algorithms.MigrationGuide)
	})
}

// providerYAML is a descriptor whose cc-utils and OCM v1 forms differ only in
// provider labels; %s is replaced by the provider string.
const providerYAML = `
meta:
  schemaVersion: v2
component:
  name: acme.org/provider
  version: 1.0.0
  provider: '%s'
  repositoryContexts: []
  componentReferences: []
  sources: []
  resources: []
`

func providerDescriptor(t *testing.T, provider string) *descruntime.Descriptor {
	t.Helper()
	var v2desc v2.Descriptor
	require.NoError(t, yaml.Unmarshal([]byte(strings.Replace(providerYAML, "%s", provider, 1)), &v2desc))
	desc, err := descruntime.ConvertFromV2(&v2desc)
	require.NoError(t, err)
	return desc
}

// TestVerifyJSONNormalisationV1FallbackRejectsInjectedProviderLabel guards the
// OCM v1 fallback against provider label injection: OCM v1 drops unsigned
// provider labels, so without a guard the tampered descriptor's OCM v1 form
// would equal the original's cc-utils form.
func TestVerifyJSONNormalisationV1FallbackRejectsInjectedProviderLabel(t *testing.T) {
	for name, tc := range map[string]struct{ signed, tampered string }{
		"empty labels": {
			signed:   `{"name":"ACME","labels":[]}`,
			tampered: `{"name":"ACME","labels":[{"name":"injected","value":"evil"}]}`,
		},
		"no labels": {
			signed:   `{"name":"ACME"}`,
			tampered: `{"name":"ACME","labels":[{"name":"injected","value":"evil"}]}`,
		},
		"signing label kept": {
			signed:   `{"name":"ACME","labels":[{"name":"p","value":"x","signing":true}]}`,
			tampered: `{"name":"ACME","labels":[{"name":"p","value":"x","signing":true},{"name":"injected","value":"evil"}]}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			digest := jsonV1DigestOf(t, providerDescriptor(t, tc.signed), ccUtilsNormalise)
			require.NoError(t, algorithms.Verify(providerDescriptor(t, tc.signed), digest))

			err := algorithms.Verify(providerDescriptor(t, tc.tampered), digest)
			require.ErrorIs(t, err, algorithms.ErrDigestMismatch)
			require.ErrorContains(t, err, "re-sign")
		})
	}

	t.Run("ambiguous OCM v1 signature fails with re-sign error", func(t *testing.T) {
		desc := providerDescriptor(t, `{"name":"ACME","labels":[{"name":"unsigned","value":"y"}]}`)
		require.ErrorIs(t, jsonnormv1.OCMv1Applicable(desc), jsonnormv1.ErrAmbiguousProviderLabels)
		err := algorithms.Verify(desc, jsonV1DigestOf(t, desc, jsonnormv1.NormaliseOCMv1))
		require.ErrorIs(t, err, algorithms.ErrDigestMismatch)
		require.ErrorIs(t, err, jsonnormv1.ErrAmbiguousProviderLabels)
		require.ErrorContains(t, err, "OCM v1 variant")
		require.ErrorContains(t, err, "re-sign")
		require.ErrorContains(t, err, algorithms.MigrationGuide)
	})
}

// TestVerifyRealOCMv1SignatureWithObjectInArray uses a descriptor and digest
// written by the OCM v1 0.52.0 CLI whose signed label value is a list of
// objects: NormaliseOCMv1 reproduces the digest, but the fallback refuses it
// because the same bytes are the cc-utils form of a differently shaped value.
func TestVerifyRealOCMv1SignatureWithObjectInArray(t *testing.T) {
	r := require.New(t)
	data, err := os.ReadFile("testdata/ocmv1-list-of-maps.yaml")
	r.NoError(err)
	var v2desc v2.Descriptor
	r.NoError(yaml.Unmarshal(data, &v2desc))
	desc, err := descruntime.ConvertFromV2(&v2desc)
	r.NoError(err)
	expected := descruntime.Digest{
		HashAlgorithm:          "SHA-256",
		NormalisationAlgorithm: jsonV1,
		Value:                  "2f7052bd529269e2c40131a3a7149486f88d67f8fbefebbbc482ca0c78d3f758",
	}

	r.Equal(expected.Value, jsonV1DigestOf(t, desc, jsonnormv1.NormaliseOCMv1).Value)
	r.NotEqual(expected.Value, jsonV1DigestOf(t, desc, ccUtilsNormalise).Value, "must reach the OCM v1 fallback")
	err = algorithms.Verify(desc, expected)
	r.ErrorIs(err, algorithms.ErrDigestMismatch)
	r.ErrorIs(err, jsonnormv1.ErrAmbiguousArrayObjects)
	r.ErrorContains(err, "re-sign")
	r.ErrorContains(err, algorithms.MigrationGuide)
}

// TestVerifyJSONNormalisationV1FallbackRejectsReshapedArray: a cc-utils
// signature over a list of single-key objects must not verify the value
// reshaped into the objects OCM v1 would rewrite to those bytes.
func TestVerifyJSONNormalisationV1FallbackRejectsReshapedArray(t *testing.T) {
	r := require.New(t)
	signed := numberDescriptor(t, `[[{"a": 2}, {"b": 1}]]`)
	tampered := numberDescriptor(t, `[{"b": 1, "a": 2}]`)
	digest := jsonV1DigestOf(t, signed, ccUtilsNormalise)
	r.NoError(algorithms.Verify(signed, digest))
	r.Equal(digest.Value, jsonV1DigestOf(t, tampered, jsonnormv1.NormaliseOCMv1).Value, "the reshaped value collides in the OCM v1 form")

	err := algorithms.Verify(tampered, digest)
	r.ErrorIs(err, algorithms.ErrDigestMismatch)
	r.ErrorIs(err, jsonnormv1.ErrAmbiguousArrayObjects)
}
