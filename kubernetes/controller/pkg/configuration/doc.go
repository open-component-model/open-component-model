// Package configuration provides functions for loading and merging OCM
// configurations from Kubernetes Secrets and ConfigMaps.
//
// It supports native OCM config entries as well as Docker-style registry
// credentials, flattens multiple configuration sources into a single
// [Configuration] with a content-addressable hash, and enforces a strict
// allowlist of accepted config types.
//
// WARNING: This package is part of the public API surface of the OCM Kubernetes
// controller but is still under active development. APIs may change in
// backwards-incompatible ways at any time. We make no stability guarantees.
package configuration
