// Package jcs implements JSON Canonicalization Scheme (JCS) as defined in RFC 8785.
// JCS is a deterministic transformation of JSON documents that ensures that logically
// equivalent documents have the same byte representation.
//
// The package provides a comprehensive set of tools for normalizing JSON data:
// - JSON canonicalization according to RFC 8785
// - Flexible exclusion rules for filtering JSON structures
// - Support for custom value mapping and transformation
// - Handling of special cases like empty values and null fields
//
// Key Features:
// - Implements the complete JCS specification from RFC 8785
// - Supports both map and array structures
// - Provides configurable exclusion rules
// - Handles nested JSON structures
// - Maintains consistent ordering of map keys
// - Preserves JSON data types
//
// Exclusion Rules:
// The package provides several types of exclusion rules for fine-grained control over normalization:
//
//  1. MapExcludes: Excludes specific fields from maps
//     excludes := MapExcludes{
//     "field1": nil,  // Exclude field1
//     "field2": NoExcludes{},  // Include field2
//     }
//
//  2. MapIncludes: Only includes specified fields from maps
//     includes := MapIncludes{
//     "field1": NoExcludes{},  // Include field1
//     "field2": NoExcludes{},  // Include field2
//     }
//
//  3. ArrayExcludes: Applies rules to all elements in an array
//     excludes := ArrayExcludes{
//     Continue: NoExcludes{},  // Rules for array elements
//     }
//
//  4. DynamicArrayExcludes: Applies dynamic rules to array elements
//     excludes := DynamicArrayExcludes{
//     ValueChecker: func(v interface{}) bool { return v == nil },
//     ValueMapper:  func(v interface{}) interface{} { return v },
//     Continue:     NoExcludes{},
//     }
//
//  5. ExcludeEmpty: Removes empty values (nil, empty maps, empty arrays)
//     excludes := ExcludeEmpty{ExcludeRules: NoExcludes{}}
//
//  6. MapValue: Transforms values before applying exclusion rules
//     mapper := MapValue{
//     Mapping: func(v interface{}) interface{} { return v },
//     Continue: NoExcludes{},
//     }
//
// Usage:
//
//	The package can be used to normalize JSON data in various ways:
//
//	import (
//	    "ocm.software/open-component-model/bindings/go/descriptor/normalisation/engine/jcs"
//	)
//
//	// Basic normalization
//	normalized, err := jcs.Normalise(input, nil)
//
//	// With exclusion rules
//	excludes := jcs.MapExcludes{
//	    "field1": nil,  // Exclude field1
//	}
//	normalized, err := jcs.Normalise(input, excludes)
//
//	// With custom value mapping
//	mapper := jcs.MapValue{
//	    Mapping: func(v interface{}) interface{} {
//	        // Transform value
//	        return transformed
//	    },
//	}
//	normalized, err := jcs.Normalise(input, mapper)
//
// The package implements several key interfaces:
// - Normalisation: For creating normalized structures
// - Normalised: For working with normalized values
// - ExcludeRules: For defining exclusion behavior
// - ValueMappingRule: For custom value transformations
// - NormalisationFilter: For post-processing normalized structures
//
// For more information about JCS, see RFC 8785:
// https://datatracker.ietf.org/doc/html/rfc8785
package jcs
