// Package catalog lists OCM component names stored in remote OCI registries
// that implement the standard Distribution catalog endpoint (GET /v2/_catalog).
//
// OCM maps a component name to an OCI repository as
// <subPath>/component-descriptors/<component-name>. This package enumerates
// the repositories of a registry through ORAS remote.Registry.Repositories and
// maps them back to component names. It keeps the repositories that carry the
// component-descriptors/ segment, strips the optional subPath prefix and the
// component-descriptors/ prefix, and returns the remainder as the component
// name.
//
// The package has three parts:
//
//   - RepositoryCataloger is the backend contract. It yields raw repository
//     names, paginated, with the same callback shape as the component lister.
//   - ComponentLister wraps a RepositoryCataloger and implements
//     repository.ComponentLister. It does the component-name mapping.
//   - DistributionCataloger is the single RepositoryCataloger. It lists
//     repositories through the Distribution /v2/_catalog endpoint via ORAS.
//     NewDistributionCataloger builds it from CatalogerOptions.
package catalog
