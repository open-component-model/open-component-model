// Package v1alpha1 is the wget behavioural configuration carried inside the
// central generic OCM configuration (generic.config.ocm.software/v1). It is a
// peer of http.config.ocm.software/v1alpha1: the HTTP config controls transport
// (timeouts, TLS, retries), this config controls wget semantics — which URL
// carries which checksum, when to fail on missing verification metadata, and
// per-host overrides.
//
// The same config type is honored by both the wget input method (Wget/v1 in
// the constructor) and the wget access resource repository (the Wget/v1 access
// type on an existing component version), so descriptor authors and operators
// steer both paths through a single knob.
//
// Precedence for the effective ChecksumPolicy at a given wget URL is (tightest
// wins):
//
//  1. A [ChecksumPolicy] declared inline on the resource spec (input
//     side today; access side may follow).
//  2. A per-host [HostConfig.ChecksumPolicy] whose key matches the URL's host
//     (`host` or `host:port`; port-qualified entries win over bare hostnames).
//  3. The top-level [Config.DefaultChecksumPolicy].
//  4. Nil — compute the storage digest without external verification (today's
//     zero-config behaviour).
package v1alpha1
