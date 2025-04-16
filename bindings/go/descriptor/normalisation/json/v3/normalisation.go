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
const Algorithm = "jsonNormalisation/v3"

// init registers the normalisation algorithm on package initialization.
func init() {
	norms.Normalisations.Register(Algorithm, algo{})
}

// algo implements the normalisation interface for JSON canonicalization.
type algo struct{}

// Normalise performs normalisation on the given descriptor using the default type and exclusion rules.
func (m algo) Normalise(cd *descruntime.Descriptor) ([]byte, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	desc, err := descruntime.ConvertToV2(scheme, cd)
	if err != nil {
		return nil, err
	}
	defaultComponent(desc)
	return jcs.Normalise(desc, CDExcludes)
}

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

func providerMapper(v interface{}) interface{} {
	var provider map[string]interface{}
	err := json.Unmarshal([]byte(v.(string)), &provider)
	if err == nil {
		return provider
	}
	return v
}
