// Package v1 contains the contracts and types for OCM plugins.
//
// It defines interfaces for interacting with OCM repositories and resources,
// enabling plugins to read from and write to them.
//
// The contracts are categorized based on their functionality:
//
//   - IdentityProvider: Provides a way to get the identity of a typed object.
//   - ResourceInputPluginContract: Defines methods for processing resources.
//   - SourceInputPluginContract: Defines methods for processing sources.
//   - ResourceDigestProcessorPlugin: Defines methods for processing resource digests.
//   - ConstructionContract: Grouping interface for use by the implementation.
//
// The types define the request and response structures used by these contracts.
package v1
