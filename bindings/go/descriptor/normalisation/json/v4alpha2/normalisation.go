package v4alpha2

import (
	norms "ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/internal/common"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Algorithm is the registered name for this normalisation algorithm. It is the
// faithful re-implementation of legacy jsonNormalisation/v3 and the target the
// "jsonNormalisation/v3" alias resolves to (see signing.LegacyNormalisationAlgo).
//
// It is v4alpha1's JCS field selection with a single correction: label.version
// is signed again (v4alpha1 dropped it). That makes v4alpha2 byte-identical to
// legacy ocm's jsonNormalisation/v3 — including reference labels, which v3 (and
// v4alpha1) emit unfiltered. v4alpha3 is the forward-looking default that, on
// top of this, filters reference labels signing-only.
const Algorithm = "jsonNormalisation/v4alpha2"

// ExclusionRules defines which fields to exclude from the normalised output. It
// is identical to v4alpha1's rules except for label handling:
//
//   - LabelExcludes keeps label.version (v4alpha1 drops it). Legacy v3, cc-utils
//     and jsonNormalisation/v1 all sign it, so keeping it realigns v4alpha2 with
//     v3 at the component, provider, resource and source positions.
//   - componentReferences stays unfiltered (jcs.NoExcludes): every reference
//     label — signing and non-signing — is emitted with every field, version
//     included. This reproduces legacy v3 byte-for-byte: v3 kept reference
//     labels unfiltered (its rule was keyed "references" while the JSON key is
//     "componentReferences", so it never matched). v4alpha1 keyed it correctly
//     only to strip label.version; v4alpha2 drops that strip to match v3.
//     Signing-only filtering of reference labels is the deliberate change in
//     jsonNormalisation/v4alpha3.
//
// All other field selection (drop repositoryContexts/sources.access/srcRefs/
// signatures/nestedDigests, provider-as-map, resource none-access handling,
// JCS/RFC-8785 canonicalisation) is unchanged from v4alpha1.
var ExclusionRules = jcs.MapExcludes{
	"meta": nil,
	"component": jcs.MapExcludes{
		"repositoryContexts": nil,
		"provider": jcs.MapValue{
			Mapping: common.ProviderAsMap,
			Continue: jcs.MapExcludes{
				"labels": LabelExcludes,
			},
		},
		"labels": LabelExcludes,
		"resources": jcs.DynamicArrayExcludes{
			ValueMapper: common.MapResourcesWithNoneAccess,
			Continue: jcs.MapExcludes{
				"access":  nil,
				"srcRefs": nil,
				"labels":  LabelExcludes,
			},
		},
		"sources": jcs.ArrayExcludes{
			Continue: jcs.MapExcludes{
				"access": nil,
				"labels": LabelExcludes,
			},
		},
		// componentReferences are emitted unfiltered to match legacy v3: keep
		// every reference and every reference label (signing and non-signing)
		// with all fields, version included. v4alpha3 applies the full
		// signing-only LabelExcludes here instead.
		"componentReferences": jcs.NoExcludes{},
	},
	"signatures":    nil,
	"nestedDigests": nil,
}

// LabelExcludes defines exclusion rules for label entries during normalization.
// It excludes labels that don't carry signing: true and, for the signed ones,
// keeps name, version, value and signing.
//
// Unlike v4alpha1, version IS kept: it is semantically part of the attribute and
// is signed by legacy jsonNormalisation/v3, cc-utils and jsonNormalisation/v1.
// v4alpha1 is the outlier that drops it; v4alpha2 realigns with that intent.
var LabelExcludes = jcs.ExcludeEmpty{
	TransformationRules: jcs.DynamicArrayExcludes{
		ValueChecker: common.IgnoreLabelsWithoutSignature,
		Continue: jcs.MapIncludes{
			"name":    jcs.NoExcludes{},
			"version": jcs.NoExcludes{},
			"value":   jcs.NoExcludes{},
			"signing": jcs.NoExcludes{},
		},
	},
}

// init registers the normalisation algorithm on package initialization.
func init() {
	norms.Normalisations.Register(Algorithm, algo{})
}

// algo implements the normalisation interface for JSON canonicalization.
type algo struct{}

// Normalise performs normalisation on the given descriptor using the v4alpha2
// exclusion rules, delegating to the shared v4-family body.
func (m algo) Normalise(cd *descruntime.Descriptor) ([]byte, error) {
	return common.Normalise(cd, ExclusionRules)
}
