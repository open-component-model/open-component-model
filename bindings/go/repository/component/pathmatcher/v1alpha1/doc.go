// Package v1alpha1 provides a mechanism for selecting OCM (Open Component Model)
// repository specifications based on component name patterns.
//
// It implements a ComponentVersionRepositorySpecProvider that uses path pattern
// matching (from github.com/gobwas/glob) in combination with the
// resolver config type to associate component names with
// repository specifications. This allows flexible configuration of which
// repository to use for resolving component versions, depending on the
// component's name.
//
// Example usage:
//
//	provider := NewSpecProvider(ctx, resolvers)
//	repoSpec, err := provider.GetRepositorySpec(ctx, identity)
//
// This package is useful when you need to route component version requests to
// different repositories based on naming conventions or patterns.
package v1alpha1
