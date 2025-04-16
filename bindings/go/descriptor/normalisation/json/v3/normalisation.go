package v3

import (
	"encoding/json"

	norms "ocm.software/open-component-model/bindings/go/descriptor/normalisation"
	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Algorithm is the registered name for this normalisation algorithm.
// It is used to identify and retrieve this specific normalisation implementation
// from the normalisation registry.
const Algorithm = "jsonNormalisation/v3"

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

// Normalise performs normalisation on the given descriptor using the default type and exclusion rules.
// It converts the descriptor to v2 format, applies default values, and then normalizes
// the JSON representation using the JCS (JSON Canonicalization Scheme) algorithm.
//
// Parameters:
//   - cd: The component descriptor to normalize
//
// Returns:
//   - []byte: The normalized JSON representation of the descriptor
//   - error: Any error that occurred during normalization
func (m algo) Normalise(cd *descruntime.Descriptor) ([]byte, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	desc, err := descruntime.ConvertToV2(scheme, cd)
	if err != nil {
		return nil, err
	}
	defaultComponent(desc)
	return jcs.Normalise(desc, CDExcludes)
}

// defaultComponent sets default values for various fields in the v2 descriptor
// if they are not already set. This ensures consistent normalization output
// regardless of whether optional fields are present in the input.
//
// Parameters:
//   - d: The v2 descriptor to set defaults for
func defaultComponent(d *v2.Descriptor) {
	component := d.Component
	if component.RepositoryContexts == nil {
		component.RepositoryContexts = make([]*runtime.Raw, 0)
	}
	if component.References == nil {
		component.References = make([]v2.Reference, 0)
	}
	if component.Sources == nil {
		component.Sources = make([]v2.Source, 0)
	}
	if component.References == nil {
		component.References = make([]v2.Reference, 0)
	}
	if component.Resources == nil {
		component.Resources = make([]v2.Resource, 0)
	}

	if d.Meta.Version == "" {
		d.Meta.Version = "v2"
	}
}

// CDExcludes defines which fields to exclude from the normalised output.
// This map specifies the exclusion rules for different parts of the component descriptor.
// IMPORTANT: If you change these, adjust the equivalent functions in the generic part.
var CDExcludes = jcs.MapExcludes{
	"meta": nil,
	"component": jcs.MapExcludes{
		"repositoryContexts": nil,
		"provider": jcs.MapValue{
			Mapping: providerMapper,
			Continue: jcs.MapExcludes{
				"labels": jcs.LabelExcludes,
			},
		},
		"labels": jcs.LabelExcludes,
		"resources": jcs.DynamicArrayExcludes{
			ValueMapper: jcs.MapResourcesWithNoneAccess,
			Continue: jcs.MapExcludes{
				"access":  nil,
				"srcRefs": nil,
				"labels":  jcs.LabelExcludes,
			},
		},
		"sources": jcs.ArrayExcludes{
			Continue: jcs.MapExcludes{
				"access": nil,
				"labels": jcs.LabelExcludes,
			},
		},
		"references": jcs.ArrayExcludes{
			Continue: jcs.MapExcludes{
				"labels": jcs.LabelExcludes,
			},
		},
	},
	"signatures":    nil,
	"nestedDigests": nil,
}

// providerMapper converts a provider string into a map structure if possible.
// This function is used during normalization to handle provider information
// in a standardized way.
func providerMapper(v interface{}) interface{} {
	var provider map[string]interface{}
	err := json.Unmarshal([]byte(v.(string)), &provider)
	if err == nil {
		return provider
	}
	return v
}
