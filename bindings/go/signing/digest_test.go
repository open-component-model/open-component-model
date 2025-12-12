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
