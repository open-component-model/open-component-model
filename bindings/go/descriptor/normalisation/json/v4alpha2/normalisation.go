package v4alpha2

import (
	norms "ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/internal/common"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Algorithm is the registered name for this normalisation algorithm. It is the
// successor to jsonNormalisation/v4alpha1 and the default for new signatures.
//
// It is identical to v4alpha1's JCS field selection except for label handling
// (see ExclusionRules and LabelExcludes): label.version is signed, and reference
// labels are filtered like every other label position. v4alpha1 stays frozen for
// verifying existing signatures.
const Algorithm = "jsonNormalisation/v4alpha2"

// ExclusionRules defines which fields to exclude from the normalised output. It
// is identical to v4alpha1's rules except for label handling:
//
//   - LabelExcludes keeps label.version (v4alpha1 drops it).
//   - componentReferences carries the full LabelExcludes rule under the correct
//     JSON key, so reference labels are filtered signing-only like component,
//     provider, resource and source labels. v4alpha1 keyed the rule "references"
//     (which never matched) and only stripped label.version as a stopgap, so it
//     still signed non-signing reference labels.
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
		// componentReferences carry labels too. Unlike v4alpha1 (which keyed the
		// rule "references", never matched, and fell back to stripping only
		// label.version), apply the full LabelExcludes under the correct JSON key
		// so reference labels are normalised exactly like every other label
		// position: signing-only inclusion, version kept.
		"componentReferences": jcs.ArrayExcludes{
			Continue: jcs.MapExcludes{
				"labels": LabelExcludes,
			},
		},
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
