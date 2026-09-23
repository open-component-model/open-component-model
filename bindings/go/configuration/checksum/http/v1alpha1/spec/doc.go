// Package spec is the checksum-over-HTTP configuration carried inside the
// central generic OCM configuration (generic.config.ocm.software/v1). It
// controls whether downloaded HTTP bytes are verified against a source-side
// checksum, with per-host overrides.
//
// The same config is honored by both the wget input method (Wget/v1 in the
// constructor) and the wget access resource repository, so descriptor authors
// and operators steer both paths through a single knob. The name is
// transport-scoped ("http"), not plugin-scoped.
//
// The reduced initial surface is a single [ChecksumMode] per policy:
// PeekWithHEADOrFail, PeekWithHEADOrCompute (the default), Compute, or
// Disable.
//
// Precedence for the effective ChecksumPolicy at a given wget URL (tightest
// wins):
//
//  1. Per-host [HostConfig.ChecksumPolicy] whose key matches the URL's host
//     (port-qualified entries win over bare hostnames).
//  2. Top-level [Config.DefaultChecksumPolicy].
//  3. Nil — compute the storage digest without external verification.
package spec
