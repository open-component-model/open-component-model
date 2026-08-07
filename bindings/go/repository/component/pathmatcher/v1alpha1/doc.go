// Package v1alpha1 provides a mechanism for selecting OCM (Open Component Model)
// repository specifications based on component name patterns and version constraints.
//
// It implements a ComponentVersionRepositorySpecProvider that uses glob-based
// pattern matching and semver constraints in combination with the resolver config
// type to associate component names and versions with repository specifications.
// This allows flexible configuration of which repository to use for resolving
// component versions, depending on the component's name and version.
//
// Example usage:
//
//	provider, err := NewSpecProvider(ctx, resolvers)
//	repoSpec, err := provider.GetRepositorySpec(ctx, identity)
//
// This package is useful when you need to route component version requests to
// different repositories based on naming conventions, patterns, or version ranges.
package v1alpha1
