// Package resolvers provides implementations for resolving component version repositories
// based on component identity. It supports two resolver types:
//
//  1. Path matcher resolvers (v1alpha1) - pattern-based component name matching using glob syntax
//  2. Fallback resolvers (v1, deprecated) - priority-based resolution without pattern matching
//
// The package consolidates resolver logic used by both the CLI and controller.
//
// Two constructors are provided:
//
//   - New takes pre-extracted resolver lists on its Options and builds the
//     resolver. Callers that already hold path matcher or fallback resolver
//     lists use New directly.
//   - NewFromConfig takes a generic OCM configuration plus an explicit
//     repository scheme, extracts both resolver lists from that configuration
//     via ExtractResolvers, and delegates to New. Config is the sole source of
//     resolver lists: supplying pre-extracted lists on Options is rejected.
//     The optional Options.ComponentPatterns still route specific component
//     references to the base repository with the highest precedence.
//
// NewFromConfig supports config-only resolution with a nil base repository and
// preserves the credentials, provider, and component patterns supplied on
// Options.
package resolvers
