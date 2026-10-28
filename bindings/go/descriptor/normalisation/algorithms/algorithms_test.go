package algorithms_test

import (
	"bytes"
	"crypto"
	"log/slog"
	"strings"
	"testing"

	"github.com/opencontainers/go-digest"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/algorithms"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
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

func TestWarnNonDefaultReferenceDigest(t *testing.T) {
	warnFor := func(d descruntime.Digest) string {
		var buf bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
		algorithms.WarnNonDefaultReferenceDigest(t.Context(), logger, d, "reference", "child")
		return buf.String()
	}
	r := require.New(t)

	r.Empty(warnFor(descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: v4alpha3, Value: "aa"}), "the default must not warn")

	out := warnFor(descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: v4alpha1, Value: "bb"})
	r.Contains(out, "level=WARN")
	r.Contains(out, "reference=child", "caller attributes identify the reference")
	r.Contains(out, "normalisationAlgorithm="+v4alpha1)
	r.Contains(out, "recommended="+v4alpha3)

	r.Contains(warnFor(descruntime.Digest{}), "normalisationAlgorithm="+algorithms.NoReferenceDigest, "a missing digest warns too")
	r.Contains(warnFor(descruntime.Digest{HashAlgorithm: "SHA-256", NormalisationAlgorithm: algorithms.LegacyV3, Value: "cc"}), "normalisationAlgorithm="+algorithms.LegacyV3, "the legacy alias is reported as written")
}
