// Package ocm contains the ComponentVersionRepository abstraction to enable the CLI to
// work with different OCM repository implementations.
// Currently, it supports:
// - configuration based resolvers using path matchers
// - fallback resolvers (deprecated, will be removed in future versions)
// - component references
// This package also provides helper functions to work with component versions.
package ocm
