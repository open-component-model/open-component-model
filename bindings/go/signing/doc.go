// Package signing defines the interface for signing and verification of Component Descriptors.
//
// It also contains signing provides utilities for generating and verifying digests of
// OCM descriptors. Digests are derived by normalising descriptors into a
// canonical JSON form and hashing the result with a supported algorithm.
// These functions are used to guarantee integrity, support signature checks,
// and validate component graph consistency.
package signing
