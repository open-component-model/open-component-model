package v4alpha1

import (
	norms "ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/json/internal/common"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
)

// Algorithm is the registered name for this normalisation algorithm.
// It is used to identify and retrieve this specific normalisation implementation
// from the normalisation registry.
//
// This algorithm is defined as the "spiritual successor" to jsonNormalisation/v3.
//
// # The baseline of this algorithm is a serialized v2 component descriptor that is normalised using jcs.Normalise
//
// Special Rules are applied to the descriptor which can be observed in ExclusionRules
const Algorithm = "jsonNormalisation/v4alpha1"

// ExclusionRules defines which fields to exclude from the normalised output.
// This map specifies the exclusion rules for different parts of the component descriptor.
//
// The meta section is completely excluded.
//
// For the component section:
// - repositoryContexts are excluded
// - provider is specially handled with a custom mapper to always be serialized as a map
// - labels use special label exclusion rules and respect if the signing attribute is not set
// - resources use dynamic array excludes with special handling for access and source references:
//   - accesses excluded
//   - srcRefs excluded
//
// - signatures are excluded
// - nestedDigests (digests stored in the parent descriptor for referenced components) are excluded
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
		// componentReferences carry labels too. Historically this rule was keyed
		// "references" while the descriptor JSON key is "componentReferences", so it
		// never matched and reference labels were emitted unfiltered. Their
		// label.version nonetheless stayed absent because the runtime<->v2
		// conversion dropped it. Now that the conversion preserves version (for
		// jsonNormalisation/v1), exclude ONLY label.version here to keep v4alpha1
		// byte-stable; do NOT apply the full LabelExcludes, which would also drop
		// non-signing reference labels and change existing digests. Proper
		// signing-only filtering for reference labels is a deliberate change
		// shipped in jsonNormalisation/v4alpha3.
		"componentReferences": jcs.ArrayExcludes{
			Continue: jcs.MapExcludes{
				"labels": jcs.ArrayExcludes{
					Continue: jcs.MapExcludes{
						"version": nil,
					},
				},
			},
		},
	},
	"signatures":    nil,
	"nestedDigests": nil,
}

// LabelExcludes defines exclusion rules for label entries during normalization.
// It excludes labels that don't have a valid signature.
//
// NOTE: version is intentionally NOT included. The runtime<->v2 label
// conversion only began preserving Label.Version to support
// jsonNormalisation/v1 (which signs it); v4alpha1 never emitted version before,
// because the field was always empty across the conversion. Including it here
// would silently change v4alpha1 digests and break existing signatures over
// versioned signing labels. Keep v4alpha1 byte-stable; signing label version is
// a deliberate, versioned change (see jsonNormalisation/v1 and
// jsonNormalisation/v4alpha2 and v4alpha3 for that behaviour).
var LabelExcludes = jcs.ExcludeEmpty{
	TransformationRules: jcs.DynamicArrayExcludes{
		ValueChecker: common.IgnoreLabelsWithoutSignature,
		Continue: jcs.MapIncludes{
			"name":    jcs.NoExcludes{},
			"value":   jcs.NoExcludes{},
			"signing": jcs.NoExcludes{},
		},
	},
}

// init registers the normalisation algorithm on package initialization.
// This ensures the algorithm is available in the normalisation registry
// when the package is imported.
func init() {
	norms.Normalisations.Register(Algorithm, algo{})
}

// algo implements the normalisation interface for JSON canonicalization.
// It provides methods to normalize component descriptors into a standardized
// JSON format.
type algo struct{}

// Normalise performs normalisation on the given descriptor using the default
// type and v4alpha1 exclusion rules, delegating to the shared v4-family body.
//
// Parameters:
//   - cd: The component descriptor to normalize
//
// Returns:
//   - []byte: The normalized JSON representation of the descriptor
//   - error: Any error that occurred during normalization
func (m algo) Normalise(cd *descruntime.Descriptor) ([]byte, error) {
	return common.Normalise(cd, ExclusionRules)
}
