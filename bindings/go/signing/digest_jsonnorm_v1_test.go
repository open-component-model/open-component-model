package signing

import (
	"crypto"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	v1 "ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/v1"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// TestGenerateAndVerifyDigest_JSONNormalisationV1 proves jsonNormalisation/v1
// resolves through the real signing entrypoints: GenerateDigest produces a v1
// digest and VerifyDigestMatchesDescriptor accepts it. Byte-exactness against
// the cc-utils oracle is locked separately in the json/v1 package; this only
// exercises the wiring (normalise → hash → compare), not a cryptographic
// signature (see the plan's non-goals).
func TestGenerateAndVerifyDigest_JSONNormalisationV1(t *testing.T) {
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

	dg, err := GenerateDigest(t.Context(), desc, logger, v1.Algorithm, crypto.SHA256.String())
	r.NoError(err)
	r.Equal(v1.Algorithm, dg.NormalisationAlgorithm)

	sig := descruntime.Signature{Name: "acme", Digest: *dg}
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
