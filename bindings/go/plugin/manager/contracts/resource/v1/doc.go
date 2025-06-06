// Package v1 contains the contracts and types for OCM plugins.
//
// It defines interfaces for interacting with OCM repositories and resources,
// enabling plugins to read from and write to them.
//
// The contracts are categorized based on their functionality:
//
//   - IdentityProvider: Provides a way to get the identity of a typed object.
//   - ReadResourcePluginContract: Defines methods for getting global resources.
//   - WriteResourcePluginContract: Defines methods for adding global resources.
//   - ReadWriteResourcePluginContract: Combines the read and write functionalities for global resources.
//
// The types define the request and response structures used by these contracts.
package v1
