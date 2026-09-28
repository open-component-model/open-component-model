// Package spec is the checksum-over-HTTP configuration carried inside the
// central generic OCM configuration (generic.config.ocm.software/v1). It
// controls whether downloaded HTTP bytes are verified against a source-side
// checksum, with per-host overrides.
//
// Both the wget input method (Wget/v1) and the wget access resource repository
// honor it, so a single knob steers both paths. The name is transport-scoped
// ("http"), not plugin-scoped. The surface is a single [ChecksumMode] per
// policy: Require, Prefer (the default), or Skip.
//
// The effective mode for a wget URL is resolved tightest-first: a matching
// per-host entry (port-qualified keys win over bare hostnames), then the
// top-level [Config.Mode], then the default Prefer.
package spec
