// Package v3 provides JSON normalisation functionality for Open Component Model descriptors.
// This package implements a normalisation algorithm that standardizes JSON representations
// of component descriptors, ensuring consistent output regardless of input formatting.
//
// The main features include:
// - JSON canonicalization of component descriptors
// - Default value handling for missing fields
// - Field exclusion rules for normalisation
// - Provider mapping support
//
// The package provides a set of predefined exclusion rules (CDExcludes) that determine
// which fields are excluded from the normalised output. These rules can be customized
// if needed.
package v3
