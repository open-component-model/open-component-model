package signing

import (
	"context"
	"crypto"
	"encoding/hex"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func TestGetSupportedHash(t *testing.T) {
	h, err := getSupportedHash(crypto.SHA256.String())
	require.NoError(t, err)
	assert.Equal(t, crypto.SHA256, h)

	_, err = getSupportedHash("unknown-hash")
	assert.Error(t, err)
}

func TestEnsureNormalisationAlgo(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	// legacy should be translated to v4alpha1
	got := ensureNormalisationAlgo(ctx, LegacyNormalisationAlgo, logger)
	assert.Equal(t, v4alpha1.Algorithm, got)

	// non-legacy should be returned unchanged
	in := "someAlgo"
	got = ensureNormalisationAlgo(ctx, in, logger)
	assert.Equal(t, in, got)
}

func TestIsSafelyDigestible(t *testing.T) {
	// happy path: reference and resource digests present when required
	comp := &descruntime.Component{
		References: []descruntime.Reference{{
			ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "ref", Version: "v1"}},
			Digest:      descruntime.Digest{HashAlgorithm: crypto.SHA256.String(), NormalisationAlgorithm: v4alpha1.Algorithm, Value: "abcd"},
		}},
		Resources: []descruntime.Resource{{
			ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "ref", Version: "v1"}},
			Access:      nil,
			Digest:      nil,
		}},
	}

	// Expect no error
	require.NoError(t, IsSafelyDigestible(comp))

	// missing reference digest fields -> error
	comp2 := &descruntime.Component{
		References: []descruntime.Reference{{
			ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "ref", Version: "v1"}},
			Digest:      descruntime.Digest{},
		}},
	}
	assert.Error(t, IsSafelyDigestible(comp2))

	// resource without access but with digest -> error
	comp3 := &descruntime.Component{
		Resources: []descruntime.Resource{{
			ElementMeta: descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: "ref", Version: "v1"}},
			Access:      nil,
			Digest:      &descruntime.Digest{HashAlgorithm: crypto.SHA256.String(), NormalisationAlgorithm: v4alpha1.Algorithm, Value: "abcd"},
		}},
	}
	assert.Error(t, IsSafelyDigestible(comp3))
}

func TestValidateDigestHashAlgorithms(t *testing.T) {
	meta := func(name string) descruntime.ElementMeta {
		return descruntime.ElementMeta{ObjectMeta: descruntime.ObjectMeta{Name: name, Version: "v1"}}
	}
	access := &runtime.Raw{Type: runtime.NewVersionedType("OCIImage", "v1")}
	digest := func(hash string) *descruntime.Digest {
		return &descruntime.Digest{HashAlgorithm: hash, NormalisationAlgorithm: "genericBlobDigest/v1", Value: "abcd"}
	}
	resource := func(name string, d *descruntime.Digest) descruntime.Resource {
		return descruntime.Resource{ElementMeta: meta(name), Access: access, Digest: d}
	}
	withResources := func(res ...descruntime.Resource) *descruntime.Component {
		return &descruntime.Component{Resources: res}
	}
	withReference := func(d *descruntime.Digest) *descruntime.Component {
		return &descruntime.Component{References: []descruntime.Reference{{ElementMeta: meta("ref"), Digest: *d}}}
	}

	tests := []struct {
		name    string
		comp    *descruntime.Component
		wantErr string
	}{
		{name: "resource SHA-256 (lowercase, no dash)", comp: withResources(resource("res", digest("sha256")))},
		{name: "resource SHA-512", comp: withResources(resource("res", digest("SHA-512")))},
		{name: "resource excluded from signature", comp: withResources(resource("res", descruntime.NewExcludeFromSignatureDigest()))},
		{name: "resource MD5", comp: withResources(resource("res", digest("MD5"))), wantErr: `"MD5" in resource for res:v1`},
		{name: "resource SHA-1", comp: withResources(resource("res", digest("SHA-1"))), wantErr: `"SHA-1" in resource for res:v1`},
		{
			name:    "MD5 behind an incomplete digest on another resource",
			comp:    withResources(resource("incomplete", nil), resource("weak", digest("MD5"))),
			wantErr: `"MD5" in resource for weak:v1`,
		},
		{name: "reference SHA-512", comp: withReference(digest("SHA-512"))},
		{name: "reference SHA-1", comp: withReference(digest("SHA-1")), wantErr: `"SHA-1" in componentReference for ref:v1`},
		{name: "reference NO-DIGEST", comp: withReference(digest(descruntime.NoDigest)), wantErr: `"NO-DIGEST" in componentReference for ref:v1`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			err := ValidateDigestHashAlgorithms(tc.comp)
			if tc.wantErr == "" {
				r.NoError(err)
				return
			}
			r.ErrorIs(err, ErrUnsupportedDigestHash)
			r.ErrorContains(err, tc.wantErr)

			// IsSafelyDigestible, which the controller enforces, rejects the
			// algorithm only in FIPS 140-3 mode.
			setFIPSMode(t, true)
			r.True(DigestHashAlgorithmsEnforced())
			r.ErrorIs(IsSafelyDigestible(tc.comp), ErrUnsupportedDigestHash)
			setFIPSMode(t, false)
			r.False(DigestHashAlgorithmsEnforced())
			r.NotErrorIs(IsSafelyDigestible(tc.comp), ErrUnsupportedDigestHash)
		})
	}
}

// setFIPSMode overrides the FIPS 140-3 mode seen by this package for the
// duration of the test, independent of how the test binary was built.
func setFIPSMode(t *testing.T, enabled bool) {
	t.Helper()
	original := fipsEnabled
	fipsEnabled = func() bool { return enabled }
	t.Cleanup(func() { fipsEnabled = original })
}

// Tests for GenerateDigest
func TestGenerateDigest_Default(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	d := &descruntime.Descriptor{
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: "test", Version: "v1"}},
			Provider:      descruntime.Provider{Name: "test-provider"},
		},
	}

	digest, err := GenerateDigest(ctx, d, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	require.NoError(t, err)
	require.NotNil(t, digest)
	assert.Equal(t, v4alpha1.Algorithm, digest.NormalisationAlgorithm)
	assert.Equal(t, crypto.SHA256.String(), digest.HashAlgorithm)
	// digest value should be a valid hex string of the correct length (sha256 -> 32 bytes -> 64 hex chars)
	b, err := hex.DecodeString(digest.Value)
	require.NoError(t, err)
	assert.Equal(t, 32, len(b))
}

func TestGenerateDigest_InvalidHash(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	d := &descruntime.Descriptor{}
	_, err := GenerateDigest(ctx, d, logger, v4alpha1.Algorithm, "unknown-hash")
	assert.Error(t, err)
}

func TestGenerateDigest_LegacyNormalisation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()
	d := &descruntime.Descriptor{
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: "n"}},
			Provider:      descruntime.Provider{Name: "legacy-provider"},
		},
	}

	digest, err := GenerateDigest(ctx, d, logger, LegacyNormalisationAlgo, crypto.SHA256.String())
	require.NoError(t, err)
	require.NotNil(t, digest)
	// ensure legacy was mapped to v4alpha1
	assert.Equal(t, v4alpha1.Algorithm, digest.NormalisationAlgorithm)
}

// Tests for VerifyDigestMatchesDescriptor
func TestVerifyDigestMatchesDescriptor_Success(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	d := &descruntime.Descriptor{
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: "cmp", Version: "v1"}},
			Provider:      descruntime.Provider{Name: "provider"},
		},
	}

	// compute canonical digest for descriptor
	dg, err := GenerateDigest(ctx, d, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	require.NoError(t, err)

	sig := descruntime.Signature{
		Name:   "s1",
		Digest: *dg,
	}

	require.NoError(t, VerifyDigestMatchesDescriptor(ctx, &descruntime.Descriptor{Component: d.Component}, sig, logger))
}

func TestVerifyDigestMatchesDescriptor_Mismatch(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	d1 := &descruntime.Descriptor{Component: descruntime.Component{ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: "a"}}, Provider: descruntime.Provider{Name: "p"}}}
	d2 := &descruntime.Descriptor{Component: descruntime.Component{ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: "b"}}, Provider: descruntime.Provider{Name: "p"}}}

	dg, err := GenerateDigest(ctx, d1, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	require.NoError(t, err)

	sig := descruntime.Signature{Name: "s1", Digest: *dg}

	err = VerifyDigestMatchesDescriptor(ctx, d2, sig, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "digest mismatch")
}

func TestVerifyDigestMatchesDescriptor_InvalidHex(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	d := &descruntime.Descriptor{Component: descruntime.Component{ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: "x"}}, Provider: descruntime.Provider{Name: "p"}}}

	sig := descruntime.Signature{Name: "s2", Digest: descruntime.Digest{HashAlgorithm: crypto.SHA256.String(), NormalisationAlgorithm: v4alpha1.Algorithm, Value: "zzzz"}}

	err := VerifyDigestMatchesDescriptor(ctx, d, sig, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decoding digest from signature failed")
}

func TestVerifyDigestMatchesDescriptor_UnsupportedHash(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	d := &descruntime.Descriptor{Component: descruntime.Component{ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: "y"}}, Provider: descruntime.Provider{Name: "p"}}}

	dg, err := GenerateDigest(ctx, d, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	require.NoError(t, err)

	// tamper with the hash algorithm to an unsupported one
	dg.HashAlgorithm = "unknown-hash"
	sig := descruntime.Signature{Name: "s3", Digest: *dg}

	err = VerifyDigestMatchesDescriptor(ctx, d, sig, logger)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported hash algorithm")
}

func TestVerifyDigestMatchesDescriptor_LegacyNormalisation(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx := context.Background()

	d := &descruntime.Descriptor{Component: descruntime.Component{ComponentMeta: descruntime.ComponentMeta{ObjectMeta: descruntime.ObjectMeta{Name: "legacy"}}, Provider: descruntime.Provider{Name: "p"}}}

	dg, err := GenerateDigest(ctx, d, logger, v4alpha1.Algorithm, crypto.SHA256.String())
	require.NoError(t, err)

	// present the signature with the legacy normalisation identifier - it should be mapped and succeed
	dg.NormalisationAlgorithm = LegacyNormalisationAlgo
	sig := descruntime.Signature{Name: "s4", Digest: *dg}

	require.NoError(t, VerifyDigestMatchesDescriptor(ctx, d, sig, logger))
}
