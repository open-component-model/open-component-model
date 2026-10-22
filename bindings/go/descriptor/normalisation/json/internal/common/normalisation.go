// Package common holds the version-agnostic primitives shared by the
// jsonNormalisation v4 algorithm family (v4alpha1, v4alpha2). The per-version
// difference lives entirely in the JCS exclusion rules each algorithm passes to
// Normalise; everything here — field defaulting, provider/resource mapping and
// the label signature check — is identical across versions. It is internal so
// only the v4 algorithm packages depend on it; neither imports the other.
package common

import (
	"encoding/json"

	"ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
	descruntime "ocm.software/open-component-model/bindings/go/descriptor/runtime"
	v2 "ocm.software/open-component-model/bindings/go/descriptor/v2"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// Constants for "none" access types.
const (
	NoneType       = "none"
	NoneLegacyType = "None"
)

// Normalise converts the descriptor to v2 format, applies default values, and
// normalises its JSON representation with the given JCS exclusion rules. This is
// the shared three-step body of the v4 algorithm family; callers supply only the
// version-specific rules.
func Normalise(cd *descruntime.Descriptor, rules jcs.TransformationRules) ([]byte, error) {
	scheme := runtime.NewScheme(runtime.WithAllowUnknown())
	desc, err := descruntime.ConvertToV2(scheme, cd)
	if err != nil {
		return nil, err
	}
	DefaultComponent(desc)
	return jcs.Normalise(desc, rules)
}

// DefaultComponent sets default values for various fields in the v2 descriptor
// if they are not already set. This ensures consistent normalization output
// regardless of whether optional fields are present in the input.
//
// Parameters:
//   - d: The v2 descriptor to set defaults for
func DefaultComponent(d *v2.Descriptor) {
	if d.Component.RepositoryContexts == nil {
		d.Component.RepositoryContexts = make([]*runtime.Raw, 0)
	}
	if d.Component.References == nil {
		d.Component.References = make([]v2.Reference, 0)
	}
	if d.Component.Sources == nil {
		d.Component.Sources = make([]v2.Source, 0)
	}
	if d.Component.Resources == nil {
		d.Component.Resources = make([]v2.Resource, 0)
	}
}

// ProviderAsMap converts a provider string into a map structure if possible.
// This function is used during normalization to handle provider information
// in a standardized way.
// ProviderAsMap tries to parse a JSON-encoded provider string into a map.
func ProviderAsMap(v any) any {
	var provider map[string]any

	switch v := v.(type) {
	case []byte:
		if err := json.Unmarshal(v, &provider); err == nil {
			return provider
		}
	case string:
		if err := json.Unmarshal([]byte(v), &provider); err == nil {
			return provider
		}
	}

	return map[string]any{
		"name": v,
	}
}

// IgnoreLabelsWithoutSignature checks if a label lacks a valid signature and should be ignored.
// Returns true if the label should be excluded from normalization.
func IgnoreLabelsWithoutSignature(v interface{}) bool {
	if m, ok := v.(map[string]interface{}); ok {
		if sig, ok := m["signing"]; ok && sig != nil {
			return sig != "true" && sig != true
		}
	}
	return true
}

// MapResourcesWithNoneAccess maps resources with "none" access, removing the digest.
// This is used to handle special cases where resources with no access type
// should have their digest removed during normalization.
func MapResourcesWithNoneAccess(v interface{}) interface{} {
	return MapResourcesWithAccessType(
		IsNoneAccessKind,
		func(v interface{}) interface{} {
			m := v.(map[string]interface{})
			delete(m, "digest")
			return m
		},
		v,
	)
}

// MapResourcesWithAccessType applies a mapper function if the access type matches.
// This is used to transform resources based on their access type.
func MapResourcesWithAccessType(test func(string) bool, mapper func(interface{}) interface{}, v interface{}) interface{} {
	access, ok := v.(map[string]interface{})["access"]
	if !ok || access == nil {
		return v
	}
	typ, ok := access.(map[string]interface{})["type"]
	if !ok || typ == nil {
		return v
	}
	if s, ok := typ.(string); ok && test(s) {
		return mapper(v)
	}
	return v
}

// IsNoneAccessKind checks if the given access type is "none".
// Supports both current and legacy access type values.
func IsNoneAccessKind(kind string) bool {
	return kind == NoneType || kind == NoneLegacyType
}
