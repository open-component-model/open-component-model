package signing

import (
	"crypto"
	"encoding/hex"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	v4alpha1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha1"
	v4alpha2 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha2"
	v4alpha3 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v4alpha3"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestSigningBoundary_JSONNormalisationV1 proves jsonNormalisation/v1 is
// verify-only and that the restriction lives at the signing boundary, not in
// digest computation: EnsureSignable refuses v1, while GenerateDigest and
// VerifyDigestMatchesDescriptor both accept it (verification, pins and
// reference digests must keep working). Byte-exactness against the cc-utils
// oracle is locked separately in the json/v1 package; this only exercises the
// signing-vs-verification split and the verify wiring (normalise → hash →
// compare), not a cryptographic signature (see the plan's non-goals).
func TestSigningBoundary_JSONNormalisationV1(t *testing.T) {
	r := require.New(t)
	logger := slog.New(slog.DiscardHandler)

	desc := &descruntime.Descriptor{
		Meta: descruntime.Meta{Version: "v2"},
		Component: descruntime.Component{
			ComponentMeta: descruntime.ComponentMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: "acme.org/root", Version: "1.0.0"},
			},
			Provider: descruntime.Provider{Name: "ACME"},
			References: []descruntime.Reference{{
				ElementMeta: descruntime.ElementMeta{
					ObjectMeta: descruntime.ObjectMeta{Name: "child", Version: "1.0.0"},
				},
				Component: "acme.org/child",
				Digest: descruntime.Digest{
					HashAlgorithm:          "SHA-256",
					NormalisationAlgorithm: v1.Algorithm,
					Value:                  "4518a3606aa1d257ff08d1760b84490ce2c741c1278217936e7abfe5135d2976",
				},
			}},
		},
	}

	// Minting a new v1 signature is refused at the signing boundary.
	r.ErrorIs(EnsureSignable(v1.Algorithm), ErrSigningUnsupported)
	// The current default algorithm is signable.
	r.NoError(EnsureSignable(v4alpha3.Algorithm))

	// GenerateDigest is a neutral compute step shared with verification, so it
	// must resolve v1 rather than block it.
	gen, err := GenerateDigest(t.Context(), desc, logger, v1.Algorithm, crypto.SHA256.String())
	r.NoError(err)
	r.Equal(v1.Algorithm, gen.NormalisationAlgorithm)

	// Verification still resolves v1. Build the expected digest the way verify
	// does (normalise → sha256).
	normalised, err := normalisation.Normalise(desc, v1.Algorithm)
	r.NoError(err)
	h := crypto.SHA256.New()
	_, err = h.Write(normalised)
	r.NoError(err)
	r.Equal(hex.EncodeToString(h.Sum(nil)), gen.Value)
	sig := descruntime.Signature{Name: "acme", Digest: descruntime.Digest{
		HashAlgorithm:          crypto.SHA256.String(),
		NormalisationAlgorithm: v1.Algorithm,
		Value:                  gen.Value,
	}}
	r.NoError(VerifyDigestMatchesDescriptor(t.Context(), desc, sig, logger))
}

// TestEnsureSignable_VerifyOnlyPolicy locks the signing boundary: the legacy and
// compatibility normalisations (jsonNormalisation/v1, v4alpha1, v4alpha2 and the
// jsonNormalisation/v3 alias) are verify-only and must be refused for new
// signatures, while the current default v4alpha3 is accepted. This enforces the
// label.version / reference-label guarantees the PR introduces: v4alpha1 drops
// label.version and v4alpha2 (= legacy v3) leaves reference labels unfiltered,
// so minting with either would reintroduce the very gaps v4alpha3 closes.
func TestEnsureSignable_VerifyOnlyPolicy(t *testing.T) {
	r := require.New(t)

	for _, algo := range []string{
		v1.Algorithm,
		v4alpha1.Algorithm,
		v4alpha2.Algorithm,
		LegacyNormalisationAlgo,
	} {
		r.ErrorIs(EnsureSignable(algo), ErrSigningUnsupported, "%s must be verify-only", algo)
	}

	r.NoError(EnsureSignable(v4alpha3.Algorithm), "the current default must be signable")
	r.Equal(v4alpha3.Algorithm, DefaultNormalisationAlgorithm, "the signing default must be v4alpha3")
}

// TestSentinelAccepted_JSONNormalisationV1 confirms a resource whose content is
// explicitly excluded from the signature (NO-DIGEST / EXCLUDE-FROM-SIGNATURE) is
// not rejected by the digest completeness and hash-algorithm checks.
func TestSentinelAccepted_JSONNormalisationV1(t *testing.T) {
	r := require.New(t)
	cd := &descruntime.Component{
		ComponentMeta: descruntime.ComponentMeta{
			ObjectMeta: descruntime.ObjectMeta{Name: "acme.org/excluded", Version: "1.0.0"},
		},
		Resources: []descruntime.Resource{{
			ElementMeta: descruntime.ElementMeta{
				ObjectMeta: descruntime.ObjectMeta{Name: "blob", Version: "1.0.0"},
			},
			Type:     "blob",
			Relation: descruntime.ExternalRelation,
			Access:   &runtime.Raw{Type: runtime.NewVersionedType("s3", "v1")},
			Digest:   descruntime.NewExcludeFromSignatureDigest(),
		}},
	}
	r.NoError(IsSafelyDigestible(cd))
	r.NoError(ValidateDigestHashAlgorithms(cd))
}
