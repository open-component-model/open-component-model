package signing

import (
	"crypto"
	"encoding/hex"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestGenerateDigestRejected_JSONNormalisationV1 proves jsonNormalisation/v1 is
// verify-only: GenerateDigest refuses to mint a new v1 digest, while
// VerifyDigestMatchesDescriptor still accepts one. Byte-exactness against the
// cc-utils oracle is locked separately in the json/v1 package; this only
// exercises the signing-vs-verification split and the verify wiring
// (normalise → hash → compare), not a cryptographic signature (see the plan's
// non-goals).
func TestGenerateDigestRejected_JSONNormalisationV1(t *testing.T) {
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

	// Signing with v1 is refused: this repo verifies legacy cc-utils signatures
	// but never produces them.
	_, err := GenerateDigest(t.Context(), desc, logger, v1.Algorithm, crypto.SHA256.String())
	r.ErrorIs(err, ErrSigningUnsupported)

	// Verification still resolves v1. Build the expected digest the way verify
	// does (normalise → sha256) — not via GenerateDigest, which is now blocked.
	normalised, err := normalisation.Normalise(desc, v1.Algorithm)
	r.NoError(err)
	h := crypto.SHA256.New()
	_, err = h.Write(normalised)
	r.NoError(err)
	sig := descruntime.Signature{Name: "acme", Digest: descruntime.Digest{
		HashAlgorithm:          crypto.SHA256.String(),
		NormalisationAlgorithm: v1.Algorithm,
		Value:                  hex.EncodeToString(h.Sum(nil)),
	}}
	r.NoError(VerifyDigestMatchesDescriptor(t.Context(), desc, sig, logger))
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
