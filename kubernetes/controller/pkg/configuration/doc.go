// Package configuration provides functions for loading and merging OCM
// configurations from Kubernetes Secrets and ConfigMaps.
//
// It supports native OCM config entries as well as docker-style registry
// credentials and
//   - flattens multiple configuration sources into a single [Configuration],
//   - calculates a content-addressable hash, and
//   - enforces a hardedcoded strict allowlist of accepted config types (config
//     types outside of the allowlist are dropped).
//
// WARNING: This package is still under active development. APIs may change in
// backwards-incompatible ways at any time. We make no stability guarantees.
package configuration
