// Package v1 contains the contracts and types for OCM plugins.
//
// It defines interfaces for interacting with OCM repositories and resources,
// enabling plugins to read from and write to them.
//
// The contracts are categorized based on their functionality:
//
//   - IdentityProvider: Provides a way to get the identity of a typed object.
//   - ReadOCMRepositoryPluginContract: Defines methods for reading component versions and local resources from an OCM repository.
//   - WriteOCMRepositoryPluginContract: Defines methods for adding component versions and local resources to an OCM repository.
//   - ReadWriteOCMRepositoryPluginContract: Combines the read and write functionalities for OCM repositories.
//
// The types define the request and response structures used by these contracts.
package v1
