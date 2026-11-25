// Package catalog lists OCM component names stored in remote OCI registries.
//
// OCM maps a component name to an OCI repository as
// <subPath>/component-descriptors/<component-name>. This package enumerates
// the repositories of a registry and maps them back to component names. It
// keeps the repositories that carry the component-descriptors/ segment, strips
// the optional subPath prefix and the component-descriptors/ prefix, and
// returns the remainder as the component name.
//
// The package has three parts:
//
//   - RepositoryCataloger is the backend contract. It yields raw repository
//     names, paginated, with the same callback shape as the component lister.
//   - ComponentLister wraps a RepositoryCataloger and implements
//     repository.ComponentLister. It does the component-name mapping.
//   - A cataloger registry dispatches by registry host. Each registry vendor
//     adds its own CatalogerFactory from an init() function, so adding a
//     registry needs no edit to a shared file. CatalogerFor returns the first
//     matching factory, or the standard /v2/_catalog cataloger
//     (NewDistributionCataloger) as the fallback.
package catalog
