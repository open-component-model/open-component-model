// Package spec is the checksum-over-HTTP configuration carried inside the
// central generic OCM configuration (generic.config.ocm.software/v1). It is a
// peer of http.config.ocm.software/v1alpha1: the HTTP config controls transport
// (timeouts, TLS, retries), this config controls how downloaded HTTP bytes
// are verified against a source-side checksum — which URL carries which
// checksum, when to fail on missing verification metadata, and per-host
// overrides. The name is transport-scoped ("http"), not plugin-scoped, so a
// future rename of the wget package to http leaves the on-the-wire identifier
// stable.
//
// The same config type is honored by both the wget input method (Wget/v1 in
// the constructor) and the wget access resource repository (the Wget/v1 access
// type on an existing component version), so descriptor authors and operators
// steer both paths through a single knob.
//
// Precedence for the effective ChecksumPolicy at a given wget URL is (tightest
// wins):
//
//  1. A per-host [HostConfig.ChecksumPolicy] whose key matches the URL's host
//     (`host` or `host:port`; port-qualified entries win over bare hostnames).
//  2. The top-level [Config.DefaultChecksumPolicy].
//  3. Nil — compute the storage digest without external verification (today's
//     zero-config behaviour).
package spec
