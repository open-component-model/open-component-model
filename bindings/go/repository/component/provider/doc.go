// Package provider provides implementations for resolving component version repositories
// based on component identity. It supports two resolver types:
//
//  1. Path matcher resolvers (v1alpha1) - pattern-based component name matching using glob syntax
//  2. Fallback resolvers (v1, deprecated) - priority-based resolution without pattern matching
//
// The package consolidates resolver logic used by both the CLI and controller.
package provider
