package spec

// ChecksumMode selects the checksum-verification posture for a wget resource.
// It is the sole verification knob: the initial configuration surface is
// deliberately reduced to a single mode, with room to grow (e.g. early
// transfer abort on descriptor digest mismatch) without another wire type.
//
// The zero value means [ChecksumModePrefer].
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
	// ChecksumModeRequire requires verification against a source-advertised
	// checksum (RFC 9530 Content-Digest or x-checksum-*) and fails when none
	// is advertised. The access side pins the digest from the advertised
	// checksum without downloading the body; the input side verifies the
	// downloaded bytes against it.
	ChecksumModeRequire ChecksumMode = "Require"
	// ChecksumModePrefer verifies against a source-advertised checksum when one
	// is available and otherwise falls back to computing SHA-256. This is the
	// default when the mode is unset. On the access side the fallback downloads
	// and hashes the body; on the input side the downloaded bytes are recorded
	// as SHA-256 without verification.
	ChecksumModePrefer ChecksumMode = "Prefer"
	// ChecksumModeSkip never consults source-advertised checksums: the body is
	// downloaded and hashed as SHA-256 without verification.
	ChecksumModeSkip ChecksumMode = "Skip"
)

// Normalize resolves the zero value to the default [ChecksumModePrefer].
func (m ChecksumMode) Normalize() ChecksumMode {
	if m == "" {
		return ChecksumModePrefer
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
	// Mode selects the verification behaviour. Defaults to "Prefer" when unset.
	// +ocm:jsonschema-gen:enum=Require,Prefer,Skip
	Mode ChecksumMode `json:"mode,omitempty"`
}
