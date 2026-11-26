package v4alpha2

import (
	norms "ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/internal/common"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Algorithm is the registered name for this normalisation algorithm, the
// re-implementation of legacy jsonNormalisation/v3 (see the package
// documentation).
const Algorithm = "jsonNormalisation/v4alpha2"

// ExclusionRules defines which fields to exclude from the normalised output. It
// differs from v4alpha1 only in label handling: LabelExcludes keeps
// label.version, and componentReferences is emitted unfiltered, as legacy v3
// does.
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
		// Unfiltered, as legacy v3: every reference label with every field.
		"componentReferences": jcs.NoExcludes{},
	},
	"signatures":    nil,
	"nestedDigests": nil,
}

// LabelExcludes defines exclusion rules for label entries during normalization.
// It excludes labels that don't carry signing: true and, for the signed ones,
// keeps name, version, value and signing.
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

// NormaliseLegacyV3 normalises cd the way legacy OCM v1 jsonNormalisation/v3
// does: the v4alpha2 rules, but a null-valued map entry (such as a signed
// label with value: null) is kept as "name":null instead of being dropped.
//
// It is not a registered algorithm and only serves to verify digests that
// record "jsonNormalisation/v3" (see algorithms.Verify). The registered
// v4alpha2 algorithm stays byte-frozen, so digests that OCM v2 recorded as
// v4alpha2 (including those minted with the v3 alias) keep verifying. For a
// descriptor without null-valued entries both produce identical bytes.
func NormaliseLegacyV3(cd *descruntime.Descriptor) ([]byte, error) {
	return common.NormaliseWith(jcs.KeepNulls, cd, ExclusionRules)
}
