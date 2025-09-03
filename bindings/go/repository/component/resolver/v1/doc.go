// Package v1 implements a component version repository with a resolver
// mechanism using regex/glob patterns for component names and semantic
// version constraints.
//
// This package provides the modern replacement for the deprecated fallback
// resolvers, offering more flexible and powerful pattern matching capabilities.
//
// The ResolverRepository allows specifying a list of repository
// specifications with priorities, component name patterns (regex/glob), and
// semantic version constraints. Based on priority and pattern matching,
// the repository determines which underlying repositories to use for
// component version operations.
package v1
