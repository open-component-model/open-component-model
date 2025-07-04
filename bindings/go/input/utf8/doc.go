// Package utf8 provides functionality for handling UTF8 string-based inputs in the Open Component Model (OCM) constructor.
//
// This package implements input methods for both resources and sources that are backed by strings interpreted
// within the constructor.
//
// Key Features:
//   - UTF8 string-based resource and source input processing
//   - Optional gzip compression support
//   - Integration with the OCM blob system for efficient data handling
//   - No credential requirements (UTF8 strings are accessed directly from the specification)
//
// Usage:
//
//	The package provides an InputMethod that can be used to process UTF8 inputs for both
//	resources and sources.
//
// Example:
//
//	result, err := (&utf8.InputMethod{}).ProcessResource(ctx, resource, nil)
//
// The package can use the v1.UTF8 specification
package utf8
