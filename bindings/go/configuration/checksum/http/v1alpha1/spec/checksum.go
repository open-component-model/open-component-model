package spec

// ChecksumMode selects how the checksum-over-HTTP verification behaves for a
// wget resource. It is the sole verification knob: the initial configuration
// surface is deliberately reduced to a single mode, with room to grow (e.g.
// early transfer abort on descriptor digest mismatch) without another wire
// type.
//
// The zero value means [ChecksumModePeekWithHEADOrCompute].
//
// Storage semantics differ per side. The input method always downloads and
// records SHA-256, regardless of which algorithm verified the transfer. The
// access-side digest processor records whichever algorithm the source
// advertises: this is safe because an access references remote bytes and any
// consumer re-fetches and re-verifies against the same source.
//
// Verification posture is a deployment concern, not a descriptor concern, so
// this type lives here rather than on the Wget/v1 input spec.
type ChecksumMode string

const (
	// ChecksumModePeekWithHEADOrFail pins the digest from what the source
	// advertises via a HEAD request (plus small externalUrl sidecar GETs) and
	// aborts when no source advertises an acceptable checksum. On the input
	// side the downloaded bytes are verified against the advertised checksum
	// and construction fails when none is available.
	ChecksumModePeekWithHEADOrFail ChecksumMode = "PeekWithHEADOrFail"
	// ChecksumModePeekWithHEADOrCompute pins the digest from what the source
	// advertises via a HEAD request and, when no source advertises a checksum,
	// falls back to downloading and hashing the body as SHA-256. This is the
	// default when the mode is unset.
	ChecksumModePeekWithHEADOrCompute ChecksumMode = "PeekWithHEADOrCompute"
	// ChecksumModeCompute skips the HEAD fast path entirely: the body is
	// always downloaded and hashed as SHA-256, without external verification.
	ChecksumModeCompute ChecksumMode = "Compute"
	// ChecksumModeDisable turns checksum processing off: the access-side
	// digest processor establishes no digest and the input side records the
	// storage digest without external verification.
	ChecksumModeDisable ChecksumMode = "Disable"
)

// Normalize resolves the zero value to the default
// [ChecksumModePeekWithHEADOrCompute].
func (m ChecksumMode) Normalize() ChecksumMode {
	if m == "" {
		return ChecksumModePeekWithHEADOrCompute
	}
	return m
}

// ChecksumPolicy is a per-host verification override carried in
// [Config.Hosts]. Its only field is the [ChecksumMode]; the top-level default
// lives on [Config.Mode].
//
// +k8s:deepcopy-gen=true
// +ocm:jsonschema-gen=true
type ChecksumPolicy struct {
	// Mode selects the verification behaviour. Defaults to
	// "PeekWithHEADOrCompute" when unset.
	// +ocm:jsonschema-gen:enum=PeekWithHEADOrFail,PeekWithHEADOrCompute,Compute,Disable
	Mode ChecksumMode `json:"mode,omitempty"`
}
